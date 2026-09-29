/* Request, RequestBody and RequestAuth. Definitions use Postman's
 * collection v2.1 shape so the Go side can convert straight to and from
 * model.Request. */

function RequestAuth (def) {
    this.update(def);
}

RequestAuth.prototype.update = function (def, type) {
    var params;

    if (!def) { return; }
    if (typeof def === 'string') { def = { type: def }; }

    type = type || def.type || this.type;
    if (!type) { return; }
    this.type = type;

    params = def[type] !== undefined ? def[type] : (def.parameters || (def.type ? undefined : def));
    if (params === undefined) { params = []; }
    if (!Array.isArray(params) && !(params instanceof NS.VariableList)) {
        params = Object.keys(params).map(function (k) { return { key: k, value: params[k], type: 'any' }; });
    }

    if (this[type] instanceof NS.VariableList) { this[type].assimilate(params); }
    else { this[type] = new NS.VariableList(this, params); }
};

RequestAuth.prototype.use = function (type, options) {
    this.type = type;
    this[type] = new NS.VariableList(this, []);
    options && this.update(options, type);
};

RequestAuth.prototype.parameters = function () {
    if (!this.type) { return undefined; }
    if (!(this[this.type] instanceof NS.VariableList)) { this[this.type] = new NS.VariableList(this, []); }

    return this[this.type];
};

RequestAuth.prototype.current = function () { return this.type; };

RequestAuth.prototype.clear = function (type) {
    type = type || this.type;
    if (this[type] instanceof NS.VariableList) { this[type].clear(); }
    if (type === this.type) { this.type = undefined; }
};

RequestAuth.prototype.toJSON = function () {
    var out = { type: this.type };

    if (this.type && this[this.type] instanceof NS.VariableList) { out[this.type] = this[this.type].toJSON(); }

    return out;
};
RequestAuth.isValidType = function (type) { return typeof type === 'string' && Boolean(type); };
RequestAuth._postman_propertyName = 'RequestAuth';
NS.RequestAuth = RequestAuth;

function RequestBody (def) {
    this.update(def);
}

RequestBody.MODES = { raw: 'raw', urlencoded: 'urlencoded', formdata: 'formdata', file: 'file', graphql: 'graphql' };

RequestBody.prototype.update = function (def) {
    if (typeof def === 'string') { def = { mode: 'raw', raw: def }; }
    def = def || {};

    this.mode = def.mode;
    this.raw = def.raw;
    this.urlencoded = new NS.QueryParamList(this, def.urlencoded || []);
    this.formdata = new NS.FormParamList(this, def.formdata || []);
    def.file !== undefined && (this.file = def.file);
    def.graphql !== undefined && (this.graphql = def.graphql);
    def.options !== undefined && (this.options = def.options);
    this.disabled = Boolean(def.disabled);
};

RequestBody.prototype.isEmpty = function () {
    switch (this.mode) {
        case 'raw': return !this.raw;
        case 'urlencoded': return !this.urlencoded.count();
        case 'formdata': return !this.formdata.count();
        case 'file': return !(this.file && this.file.src);
        case 'graphql': return !(this.graphql && this.graphql.query);
        default: return true;
    }
};

RequestBody.prototype.toString = function () {
    if (this.mode === 'raw' && this.raw !== undefined) { return String(this.raw); }
    if (this.mode === 'urlencoded') { return NS.QueryParam.unparse(this.urlencoded.all()); }
    if (this.mode === 'graphql' && this.graphql) {
        return JSON.stringify({ query: this.graphql.query, variables: this.graphql.variables });
    }

    return '';
};

RequestBody.prototype.valueOf = function () { return this.toString(); };

RequestBody.prototype.toJSON = function () {
    var out = { mode: this.mode };

    this.raw !== undefined && (out.raw = this.raw);
    this.urlencoded.count() && (out.urlencoded = this.urlencoded.toJSON());
    this.formdata.count() && (out.formdata = this.formdata.toJSON());
    this.file !== undefined && (out.file = this.file);
    this.graphql !== undefined && (out.graphql = this.graphql);
    this.options !== undefined && (out.options = this.options);
    this.disabled && (out.disabled = true);

    return out;
};
RequestBody._postman_propertyName = 'RequestBody';
NS.RequestBody = RequestBody;

function Request (def) {
    if (typeof def === 'string') { def = { url: def }; }
    def = def || {};

    var url = def.url instanceof NS.Url ? def.url : new NS.Url(def.url === undefined ? '' : def.url);

    Object.defineProperty(this, '_url', { value: url, writable: true, enumerable: false });
    Object.defineProperty(this, 'url', {
        enumerable: true,
        get: function () { return this._url; },
        /* Postman scripts sometimes assign a plain string; accept both. */
        set: function (v) { this._url = v instanceof NS.Url ? v : new NS.Url(v); }
    });

    this.method = (def.method || 'GET').toString().toUpperCase();
    this.headers = new NS.HeaderList(this, def.header || def.headers || []);
    this.body = def.body === undefined || def.body === null ? undefined : new RequestBody(def.body);
    this.auth = def.auth ? new RequestAuth(def.auth) : undefined;
    def.description !== undefined && (this.description = def.description);
    def.id !== undefined && (this.id = def.id);
    def.name !== undefined && (this.name = def.name);
    def.proxy !== undefined && (this.proxy = def.proxy);
    def.certificate !== undefined && (this.certificate = def.certificate);
}

Request.prototype.addHeader = function (header) { this.headers.add(header); };
Request.prototype.upsertHeader = function (header) { this.headers.upsert(header); };
Request.prototype.removeHeader = function (toRemove, options) {
    var key = typeof toRemove === 'string' ? toRemove : toRemove && toRemove.key,
        ignoreCase = !options || options.ignoreCase !== false;

    if (key === undefined) { return; }
    this.headers.remove(function (header) {
        return ignoreCase ? String(header.key).toLowerCase() === String(key).toLowerCase() : header.key === key;
    });
};
Request.prototype.getHeaders = function (options) {
    options = options || {};

    return this.headers.toObject(options.enabled, !options.ignoreCase, options.multiValue, options.sanitizeKeys);
};
Request.prototype.addQueryParams = function (params) { this.url.addQueryParams(params); };
Request.prototype.removeQueryParams = function (params) { this.url.removeQueryParams(params); };
Request.prototype.getHost = function () { return this.url.getHost(); };
Request.prototype.getPath = function () { return this.url.getPath(); };
Request.prototype.getQueryString = function () { return this.url.getQueryString(); };
Request.prototype.getUrl = function () { return this.url.toString(); };
Request.prototype.authorizeUsing = function (type, options) {
    if (!this.auth) { this.auth = new RequestAuth({ type: type }); }
    this.auth.use(type, options);
};
Request.prototype.size = function () {
    var body = this.body ? this.body.toString() : '',
        header = NS.Header.unparse(this.headers).length;

    return { body: body.length, header: header, total: body.length + header, source: 'COMPUTED' };
};
Request.prototype.clone = function () { return new Request(this.toJSON()); };
Request.prototype.toJSON = function () {
    var out = { method: this.method, url: this.url.toJSON(), header: this.headers.toJSON() };

    this.body !== undefined && (out.body = this.body.toJSON());
    this.auth !== undefined && (out.auth = this.auth.toJSON());
    this.description !== undefined && (out.description = this.description);
    this.id !== undefined && (out.id = this.id);
    this.name !== undefined && (out.name = this.name);
    this.proxy !== undefined && (out.proxy = this.proxy);
    this.certificate !== undefined && (out.certificate = this.certificate);

    return out;
};
Request.prototype.toString = function () { return this.method + ' ' + this.url.toString(); };
Request.isRequest = function (obj) {
    return Boolean(obj) && obj instanceof Request;
};
Request._postman_propertyName = 'Request';
NS.Request = Request;
