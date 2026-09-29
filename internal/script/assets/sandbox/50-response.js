/* Response, matching postman-collection's accessors so the chai-postman
 * assertions (status, ok, header, body, jsonBody, jsonSchema, responseTime,
 * responseSize) behave exactly as they do in Postman. */

var REASONS = {
    100: 'Continue', 101: 'Switching Protocols', 102: 'Processing', 103: 'Early Hints',
    200: 'OK', 201: 'Created', 202: 'Accepted', 203: 'Non-Authoritative Information',
    204: 'No Content', 205: 'Reset Content', 206: 'Partial Content', 207: 'Multi-Status',
    208: 'Already Reported', 226: 'IM Used',
    300: 'Multiple Choices', 301: 'Moved Permanently', 302: 'Found', 303: 'See Other',
    304: 'Not Modified', 305: 'Use Proxy', 307: 'Temporary Redirect', 308: 'Permanent Redirect',
    400: 'Bad Request', 401: 'Unauthorized', 402: 'Payment Required', 403: 'Forbidden',
    404: 'Not Found', 405: 'Method Not Allowed', 406: 'Not Acceptable',
    407: 'Proxy Authentication Required', 408: 'Request Timeout', 409: 'Conflict',
    410: 'Gone', 411: 'Length Required', 412: 'Precondition Failed', 413: 'Payload Too Large',
    414: 'URI Too Long', 415: 'Unsupported Media Type', 416: 'Range Not Satisfiable',
    417: 'Expectation Failed', 418: "I'm a teapot", 421: 'Misdirected Request',
    422: 'Unprocessable Entity', 423: 'Locked', 424: 'Failed Dependency', 425: 'Too Early',
    426: 'Upgrade Required', 428: 'Precondition Required', 429: 'Too Many Requests',
    431: 'Request Header Fields Too Large', 451: 'Unavailable For Legal Reasons',
    500: 'Internal Server Error', 501: 'Not Implemented', 502: 'Bad Gateway',
    503: 'Service Unavailable', 504: 'Gateway Timeout', 505: 'HTTP Version Not Supported',
    506: 'Variant Also Negotiates', 507: 'Insufficient Storage', 508: 'Loop Detected',
    510: 'Not Extended', 511: 'Network Authentication Required'
};

/* chai-postman spells this reason "Unauthorised" as well as "Unauthorized";
 * both assertions compare upper-cased strings, so the table value wins. */
function standardName (code) { return REASONS[code] || ''; }

function Response (def) {
    def = def || {};
    this.id = def.id;
    this.code = def.code;
    this.status = def.reason !== undefined ? def.reason :
        (def.status !== undefined && def.status !== '' ? def.status : standardName(def.code));
    this.headers = new NS.HeaderList(this, def.header || def.headers || []);
    this.cookies = new NS.CookieList(this, def.cookie || def.cookies || []);
    this.body = def.body;
    this.responseTime = def.responseTime;
    this.responseSize = def.responseSize;
    this.originalRequest = def.originalRequest ? new NS.Request(def.originalRequest) : undefined;
    def.downloadedBytes !== undefined && (this.downloadedBytes = def.downloadedBytes);
    def.headerSize !== undefined &&
        Object.defineProperty(this, '_headerSize', { value: def.headerSize, enumerable: false });
}

Response.prototype.reason = function () { return this.status || standardName(this.code); };

Response.prototype.details = function () {
    var name = standardName(this.code);

    return { name: this.status || name, detail: '', code: this.code, standardName: name };
};

Response.prototype.text = function () { return this.body; };

Response.prototype.json = function (reviver) {
    var text = this.text();

    try {
        return JSON.parse(text === undefined || text === null ? '' : text, reviver);
    }
    catch (e) {
        /* chai-postman and user scripts branch on this name, as they do with
         * the liquid-json parser Postman uses. */
        var err = new Error(e.message);

        err.name = 'JSONError';

        throw err;
    }
};

Response.prototype.jsonp = function (reviver) {
    var text = String(this.text() || ''),
        start = text.indexOf('('),
        end = text.lastIndexOf(')');

    if (start < 0 || end < start) { return this.json(reviver); }

    return new Response({ code: this.code, body: text.slice(start + 1, end) }).json(reviver);
};

Response.prototype.contentInfo = function () {
    var ct = this.headers.get('content-type') || '',
        charset = /charset=([^;]+)/i.exec(ct);

    return {
        mimeType: ct.split(';')[0].trim(),
        contentType: ct,
        charset: charset ? charset[1].trim() : 'utf8',
        fileExtension: '',
        fileName: 'response'
    };
};

Response.prototype.size = function () {
    var contentLength = this.headers.get('content-length'),
        info = { body: 0, header: 0, total: 0 };

    if (typeof this.downloadedBytes === 'number') { info.body = this.downloadedBytes; }
    else if (typeof this.responseSize === 'number') { info.body = this.responseSize; }
    else if (contentLength && !isNaN(Number(contentLength))) { info.body = parseInt(contentLength, 10); }
    else if (this.body !== undefined && this.body !== null) { info.body = String(this.body).length; }

    info.header = typeof this._headerSize === 'number' ? this._headerSize :
        ('HTTP/X.X ' + this.code + ' ' + this.reason() + '\r\n\r\n').length + this.headers.contentSize();
    info.total = info.body + info.header;

    return info;
};

Response.prototype.dataURI = function () {
    return 'data:' + (this.contentInfo().mimeType || 'text/plain') + ';base64,' + NS.btoa(String(this.body || ''));
};

Response.prototype.toJSON = function () {
    return {
        id: this.id,
        code: this.code,
        status: this.status,
        header: this.headers.toJSON(),
        cookie: this.cookies.toJSON(),
        body: this.body,
        responseTime: this.responseTime,
        responseSize: this.responseSize
    };
};

Response.prototype.toString = function () { return this.code + ' ' + this.reason(); };
Response.isResponse = function (obj) { return Boolean(obj) && obj instanceof Response; };
Response._postman_propertyName = 'Response';
NS.Response = Response;
