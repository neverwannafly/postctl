/* Sandbox bootstrap: installs the globals a Postman script expects and
 * exposes the three entry points the Go side calls. */

function installConsole () {
    function emit (level) {
        return function () {
            host.console(level, NS.format(Array.prototype.slice.call(arguments)));
        };
    }

    var c = {
        log: emit('log'),
        info: emit('info'),
        warn: emit('warn'),
        error: emit('error'),
        debug: emit('debug')
    };

    c.trace = c.debug;
    c.dir = c.log;
    /* Accepted and ignored: postctl has no grouping or timing console. */
    c.group = c.log;
    c.groupEnd = function () {};
    c.table = c.log;
    c.time = function () {};
    c.timeEnd = function () {};
    c.assert = function (ok) {
        if (ok) { return; }
        c.error.apply(null, ['Assertion failed:'].concat(Array.prototype.slice.call(arguments, 1)));
    };

    G.console = c;
}

function installCrypto () {
    var crypto = G.crypto && typeof G.crypto === 'object' ? G.crypto : {};

    crypto.getRandomValues = function (array) {
        if (!array || typeof array.length !== 'number') {
            throw new TypeError('crypto.getRandomValues: argument must be an integer-typed TypedArray');
        }

        var bytes = host.randomBytes(array.length),
            i;

        for (i = 0; i < array.length; i++) { array[i] = bytes[i]; }

        return array;
    };

    crypto.randomUUID = function () {
        var b = host.randomBytes(16),
            hex = [],
            i;

        b[6] = (b[6] & 0x0f) | 0x40;
        b[8] = (b[8] & 0x3f) | 0x80;
        for (i = 0; i < 16; i++) { hex.push((b[i] + 0x100).toString(16).slice(1)); }

        return hex.slice(0, 4).join('') + '-' + hex.slice(4, 6).join('') + '-' + hex.slice(6, 8).join('') +
            '-' + hex.slice(8, 10).join('') + '-' + hex.slice(10, 16).join('');
    };

    G.crypto = crypto;
}

/* The only modules a script may require, matching Postman's sandbox. */
var ALLOWED = ['lodash', 'moment', 'crypto-js', 'uuid', 'chai', 'ajv', 'tv4', 'xml2js',
    'postman-collection', 'chai-postman', 'buffer'];

function installRequire () {
    var native = G.require;

    G.require = function (name) {
        var key = String(name);

        if (ALLOWED.indexOf(key) === -1) {
            throw new Error("Cannot find module '" + key + "'. postctl bundles: " + ALLOWED.join(', ') + '.');
        }

        return native(key);
    };
}

/**
 * Called once per VM, before any script runs.
 *
 * @param {Object} bridge the Go host functions
 * @param {String} contextJSON request, response, cookies and pm.info
 * @returns {undefined}
 */
G.__postctl_init = function (bridge, contextJSON) {
    host = bridge;

    installConsole();
    installCrypto();
    installRequire();

    var ctx = JSON.parse(contextJSON),
        pm = NS.buildPm(ctx),
        legacy = NS.buildLegacy(pm, ctx);

    G.atob = NS.atob;
    G.btoa = NS.btoa;
    G.pm = pm;
    G.postman = legacy.postman;

    Object.keys(legacy).forEach(function (key) {
        if (key !== 'postman') { G[key] = legacy[key]; }
    });

    NS.pm = pm;
    NS.legacy = legacy;
};

/**
 * Runs the chain of compiled scripts in order, stopping at the first failure
 * or at pm.execution.skipRequest.
 *
 * @param {Array<Function>} fns async wrappers, one per script
 * @returns {Promise} settles when the chain is done
 */
G.__postctl_run = function (fns) {
    var i = 0;

    function step () {
        if (i >= fns.length || NS.stopChain) { return Promise.resolve(); }

        var index = i++;

        return Promise.resolve()
            .then(function () { return fns[index](); })
            .then(step, function (err) {
                if (err && err.__postctlSkip) { return undefined; }
                host.scriptError(index, errName(err), errMessage(err), err && err.stack ? String(err.stack) : '');

                return undefined;
            });
    }

    return step();
};

function errName (err) {
    if (err && typeof err === 'object' && err.name) { return String(err.name); }

    return 'Error';
}

function errMessage (err) {
    if (err === null || err === undefined) { return String(err); }
    if (typeof err === 'object') { return String(err.message !== undefined ? err.message : err); }

    return String(err);
}

/**
 * Called after the event loop has drained: settles outstanding tests, folds
 * in the legacy tests object and reports the possibly mutated request.
 *
 * @returns {String} JSON with the request definition
 */
G.__postctl_finish = function () {
    NS.flushPendingTests && NS.flushPendingTests();
    NS.flushLegacyTests(G.tests, NS.pm.test.index());

    return JSON.stringify({ request: NS.pm.request.toJSON() });
};

/* pm.execution.skipRequest stops the rest of the chain. */
NS.stopChain = false;
