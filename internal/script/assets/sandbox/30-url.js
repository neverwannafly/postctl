/* Url, modelled on postman-collection's: the string is parsed into protocol,
 * auth, host segments, port, path segments, a QueryParamList and a hash, and
 * toString() puts it back together. {{variable}} references are opaque: they
 * are never split on '.' or ':' and never percent-encoded. */

var PROTOCOL_RE = /^([a-zA-Z][a-zA-Z0-9+\-.]*):\/\//,
    PORT_RE = /:([0-9]+|\{\{[^}]*\}\})$/,
    PATH_VAR_RE = /^:([^/:]+)/;

/* Split on sep, but never inside a {{...}} reference. */
function splitOutsideVars (text, sep) {
    var out = [],
        cur = '',
        i = 0,
        depth = 0;

    while (i < text.length) {
        if (text.charAt(i) === '{' && text.charAt(i + 1) === '{') { depth++; cur += '{{'; i += 2; continue; }
        if (text.charAt(i) === '}' && text.charAt(i + 1) === '}' && depth > 0) {
            depth--; cur += '}}'; i += 2; continue;
        }
        if (depth === 0 && text.charAt(i) === sep) { out.push(cur); cur = ''; i++; continue; }
        cur += text.charAt(i);
        i++;
    }
    out.push(cur);

    return out;
}

function indexOutsideVars (text, ch) {
    var i = 0,
        depth = 0;

    while (i < text.length) {
        if (text.charAt(i) === '{' && text.charAt(i + 1) === '{') { depth++; i += 2; continue; }
        if (text.charAt(i) === '}' && text.charAt(i + 1) === '}' && depth > 0) { depth--; i += 2; continue; }
        if (depth === 0 && text.charAt(i) === ch) { return i; }
        i++;
    }

    return -1;
}

function Url (def) {
    this.variables = new NS.VariableList(this, []);
    this.query = new NS.QueryParamList(this, []);
    if (typeof def === 'string') { def = Url.parse(def); }
    this.update(def || {});
}

Url.prototype.update = function (def) {
    if (typeof def === 'string') { def = Url.parse(def); }
    def = def || {};

    this.protocol = def.protocol;
    this.auth = def.auth;
    this.host = def.host;
    this.port = def.port;
    this.path = def.path;
    this.hash = def.hash;
    this.query.repopulate(def.query || []);
    this.variables.repopulate(def.variable || def.variables || []);
};

Url.prototype.getHost = function () {
    if (!this.host) { return ''; }

    return Array.isArray(this.host) ? this.host.join('.') : String(this.host);
};

Url.prototype.getPath = function (unresolved) {
    if (unresolved === true || (unresolved && unresolved.unresolved)) {
        return '/' + (this.path ? this.path.join('/') : '');
    }

    var self = this,
        segments = (this.path || []).map(function (segment) {
            var m = PATH_VAR_RE.exec(segment),
                value;

            if (!m) { return segment; }
            value = self.variables.get(m[1]);

            return typeof value === 'string' && value ? value + segment.slice(m[1].length + 1) : segment;
        });

    return '/' + segments.join('/');
};

Url.prototype.getQueryString = function () {
    if (!this.query.count()) { return ''; }

    return NS.QueryParam.unparse(this.query.all());
};

Url.prototype.getPathWithQuery = function () {
    var path = this.getPath(),
        qs = this.getQueryString();

    return qs ? path + '?' + qs : path;
};

Url.prototype.getRemote = function (forcePort) {
    var host = this.getHost(),
        port = this.port && String(this.port);

    if (forcePort && !port) { port = this.protocol === 'https' ? '443' : '80'; }

    return port ? host + ':' + port : host;
};

Url.prototype.addQueryParams = function (params) {
    this.query.populate(typeof params === 'string' ? NS.QueryParam.parse(params) : params);
};

Url.prototype.removeQueryParams = function (params) {
    var keys = (Array.isArray(params) ? params : [params]).map(function (p) { return p && p.key ? p.key : p; });

    this.query.remove(function (param) { return keys.indexOf(param.key) !== -1; });
};

Url.prototype.toString = function (forceProtocol) {
    var out = '',
        protocol = this.protocol,
        auth = '',
        qs;

    forceProtocol && !protocol && (protocol = 'http');
    if (protocol) { out += protocol + '://'; }

    if (this.auth) {
        if (typeof this.auth.user === 'string') { auth = this.auth.user; }
        if (typeof this.auth.password === 'string') { auth += ':' + this.auth.password; }
        if (auth) { out += auth + '@'; }
    }

    if (this.host) { out += this.getHost(); }
    if (this.port !== undefined && this.port !== null && this.port !== '') { out += ':' + String(this.port); }
    if (this.path) { out += this.getPath(true); }

    if (this.query && this.query.count()) {
        qs = this.getQueryString();
        if (qs === '') {
            qs = this.query.find(function (p) { return !(p && p.disabled); }) !== undefined ? '' : undefined;
        }
        if (typeof qs === 'string') { out += '?' + qs; }
    }

    if (typeof this.hash === 'string') { out += '#' + this.hash; }

    return out;
};

Url.prototype.valueOf = function () { return this.toString(); };

Url.prototype.toJSON = function () {
    var out = { raw: this.toString() };

    this.protocol !== undefined && (out.protocol = this.protocol);
    this.auth !== undefined && (out.auth = this.auth);
    this.host !== undefined && (out.host = this.host);
    this.port !== undefined && (out.port = this.port);
    this.path !== undefined && (out.path = this.path);
    this.hash !== undefined && (out.hash = this.hash);
    out.query = this.query.toJSON();
    out.variable = this.variables.toJSON();

    return out;
};

Url.parse = function (raw) {
    var text = String(raw === undefined || raw === null ? '' : raw),
        def = {},
        m, i, authority, at, portMatch;

    m = PROTOCOL_RE.exec(text);
    if (m) { def.protocol = m[1]; text = text.slice(m[0].length); }

    i = indexOutsideVars(text, '#');
    if (i >= 0) { def.hash = text.slice(i + 1); text = text.slice(0, i); }

    i = indexOutsideVars(text, '?');
    if (i >= 0) { def.query = NS.QueryParam.parse(text.slice(i + 1)); text = text.slice(0, i); }

    i = indexOutsideVars(text, '/');
    if (i >= 0) { authority = text.slice(0, i); def.path = splitOutsideVars(text.slice(i + 1), '/'); }
    else { authority = text; }

    at = authority.lastIndexOf('@');
    if (at >= 0) {
        var creds = authority.slice(0, at).split(':');

        def.auth = { user: creds[0] };
        creds.length > 1 && (def.auth.password = creds.slice(1).join(':'));
        authority = authority.slice(at + 1);
    }

    portMatch = PORT_RE.exec(authority);
    if (portMatch) { def.port = portMatch[1]; authority = authority.slice(0, -portMatch[0].length); }

    authority !== '' && (def.host = splitOutsideVars(authority, '.'));

    def.variable = (def.path || []).reduce(function (acc, segment) {
        var pv = PATH_VAR_RE.exec(segment);

        if (pv && !acc.some(function (v) { return v.key === pv[1]; })) { acc.push({ key: pv[1] }); }

        return acc;
    }, []);

    return def;
};

Url.isUrl = function (obj) { return obj instanceof Url; };
Url._postman_propertyName = 'Url';
NS.Url = Url;
