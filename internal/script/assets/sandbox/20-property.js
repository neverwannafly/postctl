/* PropertyList and the property types built on it (Header, QueryParam,
 * Variable, Cookie, FormParam), following postman-collection's semantics:
 * an ordered `members` array plus a `reference` index that is case sensitive
 * or not, and single or multi valued, per property type. */

function indexKey (Type) { return Type._postman_propertyIndexKey || 'key'; }
function insensitive (Type) { return Boolean(Type._postman_propertyIndexCaseInsensitive); }
function multi (Type) { return Boolean(Type._postman_propertyAllowsMultipleValues); }

function refKey (Type, id) {
    return insensitive(Type) ? String(id).toLowerCase() : String(id);
}

/**
 * An ordered, indexed list of properties.
 *
 * @param {Function} Type constructor of the contained property
 * @param {Object} parent owning object, exposed as __parent
 * @param {Array|Object} populate initial members
 */
function PropertyList (Type, parent, populate) {
    this.Type = Type;
    this.members = [];
    this.reference = {};
    this.__parent = parent;
    populate && this.populate(populate);
}

PropertyList.prototype.add = function (item) {
    if (item === null || item === undefined || (typeof item === 'number' && isNaN(item))) { return; }
    this.insert(item instanceof this.Type ? item : new this.Type(item));
};

PropertyList.prototype.insert = function (item, before) {
    var index = before ? this.indexOf(before) : -1,
        id = item[indexKey(this.Type)],
        key, existing;

    if (index > -1) { this.members.splice(index, 0, item); }
    else { this.members.push(item); }

    item.__parent = this;

    if (id === undefined || id === null) { return; }
    key = refKey(this.Type, id);

    if (!multi(this.Type)) { this.reference[key] = item; return; }

    existing = this.reference[key];
    if (existing === undefined) { this.reference[key] = item; }
    else if (Array.isArray(existing)) { existing.push(item); }
    else { this.reference[key] = [existing, item]; }
};

PropertyList.prototype.append = function (item) { return this.add(item); };

PropertyList.prototype.prepend = function (item) {
    return this.insert(item instanceof this.Type ? item : new this.Type(item), this.members[0]);
};

PropertyList.prototype.upsert = function (item) {
    if (item === null || item === undefined) { return null; }

    var existing = this.one(item[indexKey(this.Type)]);

    if (existing) {
        existing.update(item);

        return false;
    }
    this.add(item);

    return true;
};

PropertyList.prototype.remove = function (predicate, context) {
    var self = this,
        key = indexKey(this.Type),
        match, fn, kept = [];

    if (typeof predicate === 'string') {
        match = refKey(this.Type, predicate);
        fn = function (item) { return refKey(self.Type, item[key]) === match; };
    }
    else if (typeof predicate === 'function') { fn = predicate; }
    else if (predicate) { fn = function (item) { return item === predicate; }; }
    else { return; }

    this.members.forEach(function (item, i) {
        if (fn.call(context || self, item, i)) { delete item.__parent; }
        else { kept.push(item); }
    });
    this.members = kept;
    this.reindex();
};

PropertyList.prototype.reindex = function () {
    var self = this;

    this.reference = {};
    this.members.forEach(function (item) {
        var id = item[indexKey(self.Type)],
            key, existing;

        if (id === undefined || id === null) { return; }
        key = refKey(self.Type, id);
        if (!multi(self.Type)) { self.reference[key] = item; return; }
        existing = self.reference[key];
        if (existing === undefined) { self.reference[key] = item; }
        else if (Array.isArray(existing)) { existing.push(item); }
        else { self.reference[key] = [existing, item]; }
    });
};

PropertyList.prototype.clear = function () {
    this.members.forEach(function (item) { delete item.__parent; });
    this.members.length = 0;
    this.reference = {};
};

PropertyList.prototype.populate = function (items) {
    if (typeof items === 'string' && typeof this.Type.parse === 'function') { items = this.Type.parse(items); }
    if (Array.isArray(items)) { items.forEach(this.add, this); }
    else if (items && typeof items === 'object') {
        Object.keys(items).forEach(function (k) {
            var def = {};

            def[indexKey(this.Type)] = k;
            def.value = items[k];
            this.add(def);
        }, this);
    }
    else if (items !== undefined && items !== null) { this.add(items); }
};

PropertyList.prototype.repopulate = function (items) { this.clear(); this.populate(items); };

PropertyList.prototype.assimilate = function (source, prune) {
    var members = source && source.members ? source.members : source,
        self = this;

    Array.isArray(members) && members.forEach(function (item) { self.upsert(item); });
    if (prune) {
        this.members = this.members.filter(function (item) {
            return (members || []).some(function (m) { return m[indexKey(self.Type)] === item[indexKey(self.Type)]; });
        });
        this.reindex();
    }
};

PropertyList.prototype.all = function () { return this.members.slice(); };

PropertyList.prototype.one = function (id) {
    var val = this.reference[refKey(this.Type, id)];

    if (multi(this.Type) && Array.isArray(val)) { return val.length ? val[val.length - 1] : undefined; }

    return val;
};

PropertyList.prototype.get = function (key) {
    var member = this.one(key);

    if (!member) { return undefined; }

    return member.valueOf();
};

PropertyList.prototype.has = function (item, value) {
    var match = typeof item === 'string' ? this.reference[refKey(this.Type, item)] :
            this.members.filter(function (m) { return m === item; }),
        i, val;

    if (!match) { return false; }
    if (arguments.length === 1) { return Boolean(Array.isArray(match) ? match.length : match); }

    if (Array.isArray(match)) {
        for (i = 0; i < match.length; i++) {
            val = typeof match[i].valueOf === 'function' ? match[i].valueOf() : match[i];
            if (val === value) { return true; }
        }

        return false;
    }

    return (typeof match.valueOf === 'function' ? match.valueOf() : match) === value;
};

PropertyList.prototype.each = function (iterator, context) {
    this.members.slice().forEach(function (m, i) { iterator.call(context || this.__parent, m, i); }, this);
};

PropertyList.prototype.map = function (iterator, context) {
    return this.members.map(function (m, i) { return iterator.call(context || this, m, i); }, this);
};

PropertyList.prototype.filter = function (rule, context) {
    return this.members.filter(function (m, i) { return rule.call(context || this, m, i); }, this);
};

PropertyList.prototype.find = function (rule, context) {
    var i;

    for (i = 0; i < this.members.length; i++) {
        if (rule.call(context || this, this.members[i], i)) { return this.members[i]; }
    }

    return undefined;
};

PropertyList.prototype.reduce = function (iterator, accumulator, context) {
    return this.members.reduce(function (acc, m, i) { return iterator.call(context || this, acc, m, i); },
        accumulator);
};

PropertyList.prototype.count = function () { return this.members.length; };
PropertyList.prototype.idx = function (index) { return this.members[index]; };
PropertyList.prototype.indexOf = function (item) {
    if (typeof item === 'string') { item = this.one(item); }

    return this.members.indexOf(item);
};

PropertyList.prototype.eachParent = function (iterator, context) {
    var parent = this.__parent,
        prev;

    while (parent) {
        iterator.call(context || this, parent, prev);
        prev = parent;
        parent = parent.__parent;
    }
};

PropertyList.prototype.toObject = function (excludeDisabled, caseSensitive, multiValue, sanitizeKeys) {
    var obj = {},
        key = indexKey(this.Type),
        sensitive = !insensitive(this.Type) || caseSensitive,
        multivalue = multi(this.Type) || multiValue;

    this.each(function (member) {
        if (!member || member[key] === undefined ||
            (excludeDisabled && member.disabled) || (sanitizeKeys && !member[key])) { return; }

        var prop = sensitive ? member[key] : String(member[key]).toLowerCase();

        if (multivalue && Object.prototype.hasOwnProperty.call(obj, prop)) {
            if (!Array.isArray(obj[prop])) { obj[prop] = [obj[prop]]; }
            obj[prop].push(member.valueOf());
        }
        else { obj[prop] = member.valueOf(); }
    });

    return obj;
};

PropertyList.prototype.toJSON = function () {
    return this.members.map(function (m) { return typeof m.toJSON === 'function' ? m.toJSON() : m; });
};

PropertyList.prototype.toString = function () {
    return this.members.map(function (m) { return String(m); }).join(', ');
};

PropertyList.isPropertyList = function (obj) { return obj instanceof PropertyList; };
PropertyList._postman_propertyName = 'PropertyList';

NS.PropertyList = PropertyList;

/* ------------------------------------------------------------------ *
 * Property types
 * ------------------------------------------------------------------ */

function defineProperty (name, ctor, indexKeyName, caseInsensitive, allowsMultiple) {
    ctor._postman_propertyName = name;
    ctor._postman_propertyIndexKey = indexKeyName;
    ctor._postman_propertyIndexCaseInsensitive = Boolean(caseInsensitive);
    ctor._postman_propertyAllowsMultipleValues = Boolean(allowsMultiple);

    return ctor;
}
NS.defineProperty = defineProperty;

function keyValueUpdate (def) {
    if (def === null || def === undefined) { return; }
    if (typeof def === 'string') { this.value = def; return; }
    if (def.key !== undefined) { this.key = def.key; }
    if (def.name !== undefined && this.name !== undefined) { this.name = def.name; }
    if (def.value !== undefined) { this.value = def.value; }
    if (def.description !== undefined) { this.description = def.description; }
    if (def.disabled !== undefined) { this.disabled = Boolean(def.disabled); }
    if (def.type !== undefined && this._keepType) { this.type = def.type; }
}

function Header (def, name) {
    if (typeof def === 'string') {
        var parsed = Header.parseSingle(def);

        def = parsed;
    }
    def = def || {};
    this.key = name !== undefined ? name : (def.key !== undefined ? def.key : '');
    this.value = def.value !== undefined ? def.value : '';
    def.description !== undefined && (this.description = def.description);
    def.disabled && (this.disabled = true);
    def.system && (this.system = true);
    def.id && (this.id = def.id);
}
Header.prototype.update = keyValueUpdate;
Header.prototype.valueOf = function () { return this.value; };
Header.prototype.toString = function () { return this.key + ': ' + this.value; };
Header.prototype.toJSON = function () {
    var out = { key: this.key, value: this.value };

    this.description !== undefined && (out.description = this.description);
    this.disabled && (out.disabled = true);
    this.system && (out.system = true);

    return out;
};
Header.parseSingle = function (line) {
    var index = String(line).indexOf(':');

    if (index < 0) { return { key: String(line).trim(), value: '' }; }

    return { key: String(line).slice(0, index).trim(), value: String(line).slice(index + 1).trim() };
};
Header.parse = function (text) {
    return String(text).split(/\r?\n/).filter(function (l) { return l.trim(); }).map(Header.parseSingle);
};
Header.unparse = function (headers) {
    return (headers.members || headers).map(function (h) { return h.key + ': ' + h.value + '\r\n'; }).join('');
};
Header.create = function (value, name) { return new Header(value, name); };
Header.isHeader = function (obj) { return obj instanceof Header; };
defineProperty('Header', Header, 'key', true, true);
NS.Header = Header;

function QueryParam (def, name) {
    if (typeof def === 'string') { def = QueryParam.parseSingle(def); }
    def = def || {};
    this.key = name !== undefined ? name : (def.key !== undefined ? def.key : null);
    this.value = def.value !== undefined ? def.value : null;
    def.description !== undefined && (this.description = def.description);
    def.disabled && (this.disabled = true);
}
QueryParam.prototype.update = keyValueUpdate;
QueryParam.prototype.valueOf = function () { return this.value; };
QueryParam.prototype.toString = function () { return QueryParam.unparseSingle(this); };
QueryParam.prototype.toJSON = function () {
    var out = { key: this.key, value: this.value };

    this.description !== undefined && (out.description = this.description);
    this.disabled && (out.disabled = true);

    return out;
};
QueryParam.parseSingle = function (pair) {
    var i = String(pair).indexOf('=');

    if (i < 0) { return { key: String(pair), value: null }; }

    return { key: String(pair).slice(0, i), value: String(pair).slice(i + 1) };
};
QueryParam.parse = function (text) {
    if (!text) { return []; }

    return String(text).split('&').map(QueryParam.parseSingle);
};
QueryParam.unparseSingle = function (p) {
    if (!p || p.key === null || p.key === undefined) { return ''; }

    return p.value === null || p.value === undefined ? String(p.key) : p.key + '=' + p.value;
};
QueryParam.unparse = function (params, options) {
    var list = (params && params.members) || params || [],
        ignoreDisabled = !options || options.ignoreDisabled !== false,
        out = [];

    list.forEach(function (p) {
        if (ignoreDisabled && p.disabled) { return; }
        out.push(QueryParam.unparseSingle(p));
    });

    return out.join('&');
};
defineProperty('QueryParam', QueryParam, 'key', false, true);
NS.QueryParam = QueryParam;

function FormParam (def) {
    def = def || {};
    this.key = def.key !== undefined ? def.key : '';
    this.value = def.value !== undefined ? def.value : undefined;
    def.type !== undefined && (this.type = def.type);
    def.src !== undefined && (this.src = def.src);
    def.contentType !== undefined && (this.contentType = def.contentType);
    def.description !== undefined && (this.description = def.description);
    def.disabled && (this.disabled = true);
}
FormParam.prototype._keepType = true;
FormParam.prototype.update = keyValueUpdate;
FormParam.prototype.valueOf = function () { return this.value; };
FormParam.prototype.toString = function () { return this.key + '=' + this.value; };
FormParam.prototype.toJSON = function () {
    var out = { key: this.key };

    this.value !== undefined && (out.value = this.value);
    this.type !== undefined && (out.type = this.type);
    this.src !== undefined && (out.src = this.src);
    this.contentType !== undefined && (out.contentType = this.contentType);
    this.description !== undefined && (out.description = this.description);
    this.disabled && (out.disabled = true);

    return out;
};
defineProperty('FormParam', FormParam, 'key', false, true);
NS.FormParam = FormParam;

function Variable (def) {
    def = def || {};
    this.key = def.key !== undefined ? def.key : undefined;
    this.value = def.value;
    def.type !== undefined && (this.type = def.type);
    def.description !== undefined && (this.description = def.description);
    def.disabled && (this.disabled = true);
}
Variable.prototype._keepType = true;
Variable.prototype.update = keyValueUpdate;
Variable.prototype.get = function () { return this.value; };
Variable.prototype.set = function (value) { this.value = value; };
Variable.prototype.valueOf = function () { return this.value; };
Variable.prototype.toString = function () { return this.value === undefined ? '' : String(this.value); };
Variable.prototype.toJSON = function () {
    var out = { key: this.key, value: this.value };

    this.type !== undefined && (out.type = this.type);
    this.disabled && (out.disabled = true);

    return out;
};
defineProperty('Variable', Variable, 'key', false, false);
NS.Variable = Variable;

function Cookie (def) {
    def = def || {};
    this.name = def.name !== undefined ? def.name : def.key;
    this.key = this.name;
    this.value = def.value !== undefined ? def.value : '';
    this.domain = def.domain;
    this.path = def.path;
    this.expires = def.expires;
    this.maxAge = def.maxAge;
    this.hostOnly = Boolean(def.hostOnly);
    this.httpOnly = Boolean(def.httpOnly);
    this.secure = Boolean(def.secure);
    this.session = Boolean(def.session);
    def.extensions && (this.extensions = def.extensions);
}
Cookie.prototype.update = function (def) {
    if (!def) { return; }
    Object.keys(def).forEach(function (k) { this[k] = def[k]; }, this);
    this.key = this.name;
};
Cookie.prototype.valueOf = function () { return this.value; };
Cookie.prototype.toString = function () { return this.name + '=' + this.value; };
Cookie.prototype.toJSON = function () {
    return {
        name: this.name, value: this.value, domain: this.domain, path: this.path,
        expires: this.expires, maxAge: this.maxAge, hostOnly: this.hostOnly,
        httpOnly: this.httpOnly, secure: this.secure, session: this.session
    };
};
defineProperty('Cookie', Cookie, 'name', true, true);
NS.Cookie = Cookie;

/* Typed lists. Each keeps the PropertyList behaviour and adds the list-level
 * conveniences scripts use. */
function subList (name, Type, extras) {
    function List (parent, populate) {
        if (Array.isArray(parent) || (parent && parent.members)) { populate = parent; parent = null; }
        PropertyList.call(this, Type, parent, populate);
    }
    List.prototype = Object.create(PropertyList.prototype);
    List.prototype.constructor = List;
    List._postman_propertyName = name;
    extras && Object.keys(extras).forEach(function (k) { List.prototype[k] = extras[k]; });

    return List;
}

NS.HeaderList = subList('HeaderList', Header, {
    contentSize: function () { return Header.unparse(this).length; }
});
NS.QueryParamList = subList('QueryParamList', QueryParam);
NS.FormParamList = subList('FormParamList', FormParam);
NS.VariableList = subList('VariableList', Variable);
NS.CookieList = subList('CookieList', Cookie);
NS.CookieList.isCookieList = function (obj) { return obj instanceof NS.CookieList; };
