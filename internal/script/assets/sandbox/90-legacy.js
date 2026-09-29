/* Tier 3: the pre-pm globals that older collections still use. */

NS.buildLegacy = function (pm, ctx) {
    var legacy = {};

    legacy.postman = {
        setNextRequest: function (name) { pm.execution.setNextRequest(name); },
        getEnvironmentVariable: function (key) { return pm.environment.get(key); },
        setEnvironmentVariable: function (key, value) { pm.environment.set(key, value); },
        clearEnvironmentVariable: function (key) { pm.environment.unset(key); },
        clearEnvironmentVariables: function () { pm.environment.clear(); },
        getGlobalVariable: function (key) { return pm.globals.get(key); },
        setGlobalVariable: function (key, value) { pm.globals.set(key, value); },
        clearGlobalVariable: function (key) { pm.globals.unset(key); },
        clearGlobalVariables: function () { pm.globals.clear(); },
        getResponseHeader: function (key) {
            return pm.response ? (pm.response.headers.get(key) || null) : null;
        },
        getResponseCookie: function (name) {
            return pm.response ? (pm.response.cookies.one(name) || null) : null;
        }
    };

    legacy.tests = {};

    legacy.environment = pm.environment.toObject();
    legacy.globals = pm.globals.toObject();
    legacy.data = pm.iterationData.toObject();
    legacy.iteration = pm.info.iteration;

    legacy.request = {
        id: pm.info.requestId,
        name: pm.info.requestName,
        method: pm.request.method,
        url: pm.request.url.toString(),
        headers: pm.request.getHeaders(),
        data: pm.request.body ? pm.request.body.toString() : ''
    };

    if (pm.response) {
        legacy.responseBody = pm.response.text();
        legacy.responseCode = {
            code: pm.response.code,
            name: pm.response.details().name,
            detail: pm.response.details().detail
        };
        legacy.responseHeaders = pm.response.headers.toObject();
        legacy.responseTime = pm.response.responseTime;
        legacy.responseCookies = pm.response.cookies.toObject();
    }

    /* Postman's legacy XML helper. */
    legacy.xml2Json = function (text) {
        var out,
            error;

        G.require('xml2js').parseString(String(text), { explicitArray: false, async: false },
            function (err, result) { error = err; out = result; });
        if (error) { throw error; }

        return out;
    };

    return legacy;
};

/* Fold `tests["name"] = truthy` entries into the test results, after the
 * pm.test ones, the way Postman's legacy bridge does. */
NS.flushLegacyTests = function (tests, startIndex) {
    if (!tests) { return; }
    Object.keys(tests).forEach(function (name, i) {
        host.test(String(name), Boolean(tests[name]), false, false,
            tests[name] ? '' : 'AssertionError: expected ' + JSON.stringify(String(name)) + ' to be truthy',
            startIndex + i);
    });
};
