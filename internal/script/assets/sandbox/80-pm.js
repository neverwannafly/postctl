/* The pm object. */

/* chai plus the chai-postman plugin, loaded on first use. The plugin is the
 * real one from npm, handed a minimal sdk shim, so pm.response.to.* matches
 * Postman assertion for assertion, message for message. */
NS.chai = (function () {
    var loaded;

    return function () {
        if (loaded) { return loaded; }

        var chai = G.require('chai'),
            chaiPostman = G.require('chai-postman'),
            sdkShim = { Request: NS.Request, Response: NS.Response };

        /* Ajv is only constructed by the jsonSchema assertion, so defer the
         * cost of loading it until something actually asserts a schema. */
        function LazyAjv (options) {
            var Ajv = G.require('ajv');

            return new Ajv(options);
        }

        chai.use(chaiPostman(sdkShim, NS._(), LazyAjv));
        loaded = chai;

        return loaded;
    };
}());

/* `.to` is a getter so that merely having a response costs nothing. */
NS.attachAssertions = function (obj) {
    if (!obj) { return obj; }
    Object.defineProperty(obj, 'to', {
        configurable: true,
        get: function () { return NS.chai().expect(this).to; }
    });

    return obj;
};

function errorText (err) {
    if (!err) { return ''; }
    if (typeof err === 'string') { return err; }
    if (err.name && err.message) { return err.name + ': ' + err.message; }

    return String(err.message || err);
}

function setupTestRunner (pm) {
    var index = 0;

    function assertion (name, skipped) {
        return {
            name: String(name), async: false, skipped: Boolean(skipped),
            passed: true, error: null, index: index++, done: false
        };
    }

    function complete (data) {
        if (data.done) { return; }
        data.done = true;
        host.test(data.name, data.passed, data.skipped, data.async, errorText(data.error), data.index);
    }

    function fail (data, err) { data.error = err; data.passed = false; }

    NS.pending = [];

    pm.test = function (name, assert) {
        var data = assertion(name, false),
            returned;

        if (typeof assert !== 'function') { complete(data); return pm; }

        /* A declared parameter means the Postman done-callback style. */
        if (assert.length) {
            data.async = true;
            NS.pending.push(data);
            try {
                assert(function (err) {
                    if (data.error && data.passed === false) { return; }
                    if (err) { fail(data, err); }
                    complete(data);
                });
            }
            catch (e) { fail(data, e); complete(data); }

            return pm;
        }

        try { returned = assert(); }
        catch (e) { fail(data, e); complete(data); return pm; }

        if (returned && typeof returned.then === 'function') {
            data.async = true;
            NS.pending.push(data);
            returned.then(function () { complete(data); }, function (e) { fail(data, e); complete(data); });

            return pm;
        }

        complete(data);

        return pm;
    };

    pm.test.skip = function (name) { complete(assertion(name, true)); return pm; };
    pm.test.index = function () { return index; };

    /* Called once the event loop has drained: anything still outstanding
     * never finished, which Postman reports as a failure too. */
    NS.flushPendingTests = function () {
        NS.pending.forEach(function (data) {
            if (data.done) { return; }
            data.passed = false;
            data.error = new Error('test did not finish before the script run ended');
            data.error.name = 'AssertionError';
            complete(data);
        });
        NS.pending = [];
    };
}

function warnNoop (api) {
    return function () {
        host.console('warn', api + ' is not supported by postctl; the call did nothing.');
    };
}

NS.buildPm = function (ctx) {
    var pm = {};

    pm.info = {
        eventName: ctx.eventName,
        iteration: ctx.iteration,
        iterationCount: ctx.iterationCount,
        requestName: ctx.requestName,
        requestId: ctx.requestId,
        collectionName: ctx.collectionName
    };

    pm.globals = new NS.VariableScope('globals');
    pm.collectionVariables = new NS.VariableScope('collection');
    pm.environment = new NS.VariableScope('environment');
    pm.iterationData = new NS.VariableScope('data');
    pm.variables = new NS.VariableScope('all', 'local', true);

    pm.request = NS.attachAssertions(new NS.Request(ctx.request));
    pm.response = ctx.response ? NS.attachAssertions(new NS.Response(ctx.response)) : undefined;

    pm.cookies = new NS.CookieList(null, ctx.cookies || []);
    pm.cookies.jar = function () { return new NS.CookieJar(); };

    pm.sendRequest = function (req, callback) {
        var self = this;

        function handler () {
            return new Promise(function (resolve, reject) {
                if (!req) { reject(new Error('sendrequest: nothing to request')); return; }

                var definition;

                try {
                    definition = JSON.stringify((NS.Request.isRequest(req) ? req : new NS.Request(req)).toJSON());
                }
                catch (e) { reject(e); return; }

                /* The event loop stops once nothing is scheduled, and a
                 * request in flight on the Go side is not a job it knows
                 * about; this timer keeps the loop alive until it lands. */
                var keepAlive = G.setTimeout(function () {}, 3600000);

                host.sendRequest(definition, function (err, payload) {
                    G.clearTimeout(keepAlive);
                    if (err) { reject(new Error(err)); return; }
                    try { resolve(NS.attachAssertions(new NS.Response(JSON.parse(payload)))); }
                    catch (e) { reject(e); }
                });
            });
        }

        if (typeof callback === 'function') {
            handler().then(function (res) { callback.call(self, null, res); },
                function (err) { callback.call(self, err); });

            return self;
        }

        return handler();
    };

    var location = (ctx.location || []).slice();

    location.current = ctx.requestName;

    pm.execution = {
        location: location,
        skipRequest: function () { NS.stopChain = true; host.skipRequest(); },
        setNextRequest: function (request) {
            host.setNextRequest(request === null || request === undefined ? null : String(request));
        },
        runRequest: function () {
            host.console('warn', 'pm.execution.runRequest is not supported by postctl; the call did nothing.');

            return Promise.resolve(null);
        }
    };

    pm.visualizer = { set: warnNoop('pm.visualizer.set'), clear: function () {} };

    pm.vault = {
        get: function () {
            host.console('warn', 'pm.vault is not supported by postctl; the call resolved to undefined.');

            return Promise.resolve(undefined);
        },
        set: function () {
            host.console('warn', 'pm.vault is not supported by postctl; the call did nothing.');

            return Promise.resolve();
        },
        unset: function () {
            host.console('warn', 'pm.vault is not supported by postctl; the call did nothing.');

            return Promise.resolve();
        }
    };

    pm.require = function (name) {
        host.console('warn', 'pm.require(' + JSON.stringify(String(name)) +
            ') is not supported by postctl; it returned undefined.');

        return undefined;
    };

    Object.defineProperty(pm, 'expect', {
        enumerable: true,
        configurable: true,
        get: function () { return NS.chai().expect; }
    });

    setupTestRunner(pm);

    return pm;
};
