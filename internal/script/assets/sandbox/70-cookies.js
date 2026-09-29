/* pm.cookies.jar(): the programmatic cookie jar. Every method takes an
 * optional Node-style callback and returns a promise when none is given,
 * which is what Postman's PostmanCookieJar does. */

function callbackOrPromise (callback, work) {
    if (typeof callback === 'function') {
        try { work(callback); }
        catch (e) { callback(e); }

        return undefined;
    }

    return new Promise(function (resolve, reject) {
        work(function (err, value) { err ? reject(err) : resolve(value); });
    });
}

function jarError (op) { return new Error('cookie jar: ' + op + ': no cookie store is available'); }

function CookieJar () {}

CookieJar.prototype.set = function (url, name, value, callback) {
    var def;

    /* set(url, {name, value}, cb) | set(url, 'a=b', cb) | set(url, name, value, cb) */
    if (typeof name === 'object' && name !== null) { def = name; callback = value; }
    else if (typeof value === 'function' || value === undefined) {
        callback = value;
        def = NS.parseCookieString(String(name));
    }
    else { def = { name: String(name), value: String(value) }; }

    return callbackOrPromise(callback, function (done) {
        if (!host.cookieSet) { return done(jarError('set')); }
        var err = host.cookieSet(String(url), def);

        return err ? done(new Error(err)) : done(null, new NS.Cookie(def));
    });
};

CookieJar.prototype.get = function (url, name, callback) {
    return callbackOrPromise(callback, function (done) {
        if (!host.cookieList) { return done(jarError('get')); }
        var res = host.cookieList(String(url)),
            found;

        if (res.error) { return done(new Error(res.error)); }
        found = (res.cookies || []).filter(function (c) { return c.name === String(name); });

        return done(null, found.length ? found[found.length - 1].value : null);
    });
};

CookieJar.prototype.getAll = function (url, options, callback) {
    if (typeof options === 'function') { callback = options; }

    return callbackOrPromise(callback, function (done) {
        if (!host.cookieList) { return done(jarError('getAll')); }
        var res = host.cookieList(String(url));

        if (res.error) { return done(new Error(res.error)); }

        return done(null, new NS.CookieList(null, res.cookies || []));
    });
};

CookieJar.prototype.unset = function (url, name, callback) {
    return callbackOrPromise(callback, function (done) {
        if (!host.cookieUnset) { return done(jarError('unset')); }
        var err = host.cookieUnset(String(url), String(name));

        return err ? done(new Error(err)) : done(null);
    });
};

CookieJar.prototype.clear = function (url, callback) {
    return callbackOrPromise(callback, function (done) {
        if (!host.cookieClear) { return done(jarError('clear')); }
        var err = host.cookieClear(String(url));

        return err ? done(new Error(err)) : done(null);
    });
};

/* Postman's jar exposes these switches; postctl's jar has no per-domain
 * allow-list, so they are accepted and ignored. */
CookieJar.prototype.allowProgrammaticAccess = function () { return true; };

NS.CookieJar = CookieJar;

NS.parseCookieString = function (text) {
    var parts = String(text).split(';'),
        first = parts.shift() || '',
        eq = first.indexOf('='),
        def = { name: eq < 0 ? first.trim() : first.slice(0, eq).trim(), value: eq < 0 ? '' : first.slice(eq + 1) };

    parts.forEach(function (part) {
        var i = part.indexOf('='),
            k = (i < 0 ? part : part.slice(0, i)).trim().toLowerCase(),
            v = i < 0 ? true : part.slice(i + 1).trim();

        if (k === 'domain') { def.domain = v; }
        else if (k === 'path') { def.path = v; }
        else if (k === 'expires') { def.expires = v; }
        else if (k === 'max-age') { def.maxAge = v; }
        else if (k === 'secure') { def.secure = true; }
        else if (k === 'httponly') { def.httpOnly = true; }
    });

    return def;
};
