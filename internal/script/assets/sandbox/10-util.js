/* Small helpers shared by the sandbox: lazy require, base64 and a compact
 * util.inspect work-alike for console output. */

/* Lazily require a bundled library, caching the module on first use so a
 * script that never touches chai never pays for loading it. */
NS.lazy = function (name) {
    var mod;

    return function () {
        if (mod === undefined) { mod = G.require(name); }

        return mod;
    };
};

NS._ = NS.lazy('lodash');

var B64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

NS.btoa = function (input) {
    var str = String(input),
        out = '',
        i, c1, c2, c3;

    for (i = 0; i < str.length;) {
        c1 = str.charCodeAt(i++);
        c2 = str.charCodeAt(i++);
        c3 = str.charCodeAt(i++);
        if (c1 > 255 || c2 > 255 || c3 > 255) {
            throw new Error('btoa: The string to be encoded contains characters outside of the Latin1 range.');
        }
        out += B64.charAt(c1 >> 2);
        out += B64.charAt(((c1 & 3) << 4) | (isNaN(c2) ? 0 : c2 >> 4));
        out += isNaN(c2) ? '=' : B64.charAt(((c2 & 15) << 2) | (isNaN(c3) ? 0 : c3 >> 6));
        out += isNaN(c3) ? '=' : B64.charAt(c3 & 63);
    }

    return out;
};

NS.atob = function (input) {
    var str = String(input).replace(/[ \t\n\f\r]/g, ''),
        out = '',
        i, buf = 0, bits = 0, idx;

    if (str.length % 4 === 1) { throw new Error('atob: The string to be decoded is not correctly encoded.'); }

    for (i = 0; i < str.length; i++) {
        if (str.charAt(i) === '=') { break; }
        idx = B64.indexOf(str.charAt(i));
        if (idx < 0) { throw new Error('atob: The string to be decoded is not correctly encoded.'); }
        buf = (buf << 6) | idx;
        bits += 6;
        if (bits >= 8) {
            bits -= 8;
            out += String.fromCharCode((buf >> bits) & 0xff);
        }
    }

    return out;
};

var IDENT = /^[A-Za-z_$][A-Za-z0-9_$]*$/,
    INSPECT_DEPTH = 4,
    INSPECT_ITEMS = 100;

function quote (s) {
    var escaped = s
        .replace(/\\/g, '\\\\')
        .replace(/'/g, "\\'")
        .replace(/\n/g, '\\n')
        .replace(/\r/g, '\\r')
        .replace(/\t/g, '\\t');

    return "'" + escaped + "'";
}

function wrap (parts, open, close, prefix) {
    if (!parts.length) { return (prefix || '') + open + close; }

    return (prefix || '') + open + ' ' + parts.join(', ') + ' ' + close;
}

/* A deliberately brief stand-in for Node's util.inspect: enough structure to
 * debug a script, short enough for a console pane. */
function inspect (value, depth, seen) {
    var type = typeof value,
        parts, keys, i, n, name;

    if (value === null) { return 'null'; }
    if (type === 'undefined') { return 'undefined'; }
    if (type === 'number' || type === 'boolean') { return String(value); }
    if (type === 'bigint') { return String(value) + 'n'; }
    if (type === 'symbol') { return value.toString(); }
    if (type === 'string') { return depth === 0 ? value : quote(value); }
    if (type === 'function') {
        name = value.name;

        return name ? '[Function: ' + name + ']' : '[Function (anonymous)]';
    }

    if (value instanceof Error) { return value.stack || (value.name + ': ' + value.message); }
    if (value instanceof Date) { return isNaN(value.getTime()) ? 'Invalid Date' : value.toISOString(); }
    if (value instanceof RegExp) { return value.toString(); }

    if (seen.indexOf(value) !== -1) { return '[Circular]'; }
    if (depth >= INSPECT_DEPTH) { return Array.isArray(value) ? '[Array]' : '[Object]'; }

    seen.push(value);
    try {
        if (Array.isArray(value)) {
            parts = [];
            n = Math.min(value.length, INSPECT_ITEMS);
            for (i = 0; i < n; i++) { parts.push(inspect(value[i], depth + 1, seen)); }
            if (value.length > n) { parts.push('... ' + (value.length - n) + ' more items'); }

            return wrap(parts, '[', ']');
        }

        if (typeof Map !== 'undefined' && value instanceof Map) {
            parts = [];
            value.forEach(function (v, k) {
                parts.push(inspect(k, depth + 1, seen) + ' => ' + inspect(v, depth + 1, seen));
            });

            return wrap(parts, '{', '}', 'Map(' + value.size + ') ');
        }

        if (typeof Set !== 'undefined' && value instanceof Set) {
            parts = [];
            value.forEach(function (v) { parts.push(inspect(v, depth + 1, seen)); });

            return wrap(parts, '{', '}', 'Set(' + value.size + ') ');
        }

        keys = Object.keys(value);
        parts = [];
        n = Math.min(keys.length, INSPECT_ITEMS);
        for (i = 0; i < n; i++) {
            name = IDENT.test(keys[i]) ? keys[i] : quote(keys[i]);
            parts.push(name + ': ' + inspect(value[keys[i]], depth + 1, seen));
        }
        if (keys.length > n) { parts.push('... ' + (keys.length - n) + ' more keys'); }

        name = value.constructor && value.constructor.name;

        return wrap(parts, '{', '}', name && name !== 'Object' ? name + ' ' : '');
    }
    finally {
        seen.pop();
    }
}

NS.inspect = function (value) {
    try { return inspect(value, 0, []); }
    catch (e) { return '[unprintable: ' + (e && e.message) + ']'; }
};

/* console.log('%s scored %d', name, score), as Node's util.format does it: a
 * leading format string consumes arguments, and whatever is left is inspected
 * and appended. */
NS.format = function (args) {
    var out = [],
        next = 0,
        i;

    if (typeof args[0] === 'string' && /%[sdifjoOc%]/.test(args[0])) {
        next = 1;
        out.push(args[0].replace(/%([sdifjoOc%])/g, function (match, verb) {
            if (verb === '%') { return '%'; }
            if (next >= args.length) { return match; }

            var arg = args[next++];

            switch (verb) {
            case 's': return typeof arg === 'string' ? arg : NS.inspect(arg);
            case 'd': return typeof arg === 'bigint' ? String(arg) : String(Number(arg));
            case 'i': return String(parseInt(arg, 10));
            case 'f': return String(parseFloat(arg));
            case 'j': try { return JSON.stringify(arg); } catch (e) { return '[Circular]'; }
            case 'c': return '';
            default: return NS.inspect(arg);
            }
        }));
    }

    for (i = next; i < args.length; i++) { out.push(NS.inspect(args[i])); }

    return out.join(' ');
};
