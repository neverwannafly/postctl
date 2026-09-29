#!/usr/bin/env node
/*
 * Development-time build of the JavaScript libraries postctl embeds into its
 * goja sandbox. Run with `task assets` (or `npm ci && npm run build` here).
 * Node is never needed at run time: the outputs in bundles/ are committed and
 * embedded with go:embed.
 *
 * Each library is browserified into a standalone UMD bundle (so Node core
 * modules such as events/stream/buffer get pure-JS shims) and then minified
 * with terser targeting ES5, which is the dialect goja is happiest with.
 */
'use strict';

const browserify = require('browserify');
const terser = require('terser');
const fs = require('fs');
const path = require('path');

const OUT = path.join(__dirname, 'bundles');

// entry: what to require; standalone: the UMD global name.
const LIBS = [
  { name: 'lodash', entry: 'lodash', standalone: '_' },
  { name: 'moment', entry: 'moment', standalone: 'moment' },
  { name: 'crypto-js', entry: 'crypto-js', standalone: 'CryptoJS' },
  { name: 'uuid', entry: 'uuid', standalone: 'uuid' },
  { name: 'chai', entry: 'chai', standalone: 'chai' },
  { name: 'chai-postman', entry: 'chai-postman', standalone: 'chaiPostman' },
  { name: 'ajv', entry: 'ajv', standalone: 'Ajv' },
  { name: 'tv4', entry: 'tv4', standalone: 'tv4' },
  { name: 'xml2js', entry: 'xml2js', standalone: 'xml2js' },
  { name: 'postman-collection', entry: 'postman-collection', standalone: 'sdk' },
];

function bundle(lib) {
  return new Promise((resolve, reject) => {
    const b = browserify({
      standalone: lib.standalone,
      // goja has no DOM; make sure nothing pulls one in.
      browserField: true,
      insertGlobalVars: {
        // Keep browserify from injecting a `global.window` sniff.
        global: () => 'globalThis',
      },
    });
    b.require(require.resolve(lib.entry), { expose: lib.entry });
    b.add(require.resolve(lib.entry));
    b.bundle((err, buf) => (err ? reject(err) : resolve(buf.toString('utf8'))));
  });
}

async function main() {
  fs.mkdirSync(OUT, { recursive: true });
  const report = [];
  for (const lib of LIBS) {
    process.stdout.write(`bundling ${lib.name} ... `);
    const src = await bundle(lib);
    const min = await terser.minify(src, {
      ecma: 5,
      compress: { ecma: 5 },
      mangle: true,
      format: { ecma: 5, comments: false },
    });
    if (min.error) throw min.error;
    const file = path.join(OUT, `${lib.name}.js`);
    fs.writeFileSync(file, min.code + '\n');
    const version = require(`${lib.entry === 'uuid' ? 'uuid' : lib.entry}/package.json`).version;
    report.push({ name: lib.name, version, bytes: min.code.length });
    process.stdout.write(`${version}, ${(min.code.length / 1024).toFixed(0)} KiB\n`);
  }
  fs.writeFileSync(
    path.join(OUT, 'versions.json'),
    JSON.stringify(report, null, 2) + '\n'
  );
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
