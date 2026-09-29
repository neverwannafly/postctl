/* VariableScope: the object behind pm.environment, pm.collectionVariables,
 * pm.globals, pm.variables and pm.iterationData.
 *
 * Unlike Postman's, this one holds no values of its own. Every read and write
 * goes to the Go vars.Store, which is what the send pipeline resolves against
 * and what gets persisted afterwards (plan §0 R3). */

var VAR_REF = /\{\{([^{}]*)\}\}/g;

/**
 * @param {String} scope one of globals, collection, environment, data, local, all
 * @param {String} writeScope scope that set/unset/clear act on
 * @param {Boolean} chained true for pm.variables, which reads every scope
 */
function VariableScope (scope, writeScope, chained) {
    Object.defineProperty(this, '_scope', { value: scope, enumerable: false });
    Object.defineProperty(this, '_write', { value: writeScope || scope, enumerable: false });
    Object.defineProperty(this, '_chained', { value: Boolean(chained), enumerable: false });
}

VariableScope.prototype.get = function (key) {
    var v = host.varGet(this._scope, String(key));

    return v === null ? undefined : v;
};

VariableScope.prototype.set = function (key, value) {
    host.varSet(this._write, String(key), value === undefined ? null : value);
};

VariableScope.prototype.unset = function (key) { host.varUnset(this._write, String(key)); };

VariableScope.prototype.has = function (key) { return host.varHas(this._scope, String(key)); };

VariableScope.prototype.clear = function () { host.varClear(this._write); };

VariableScope.prototype.toObject = function () { return host.varObject(this._scope); };

VariableScope.prototype.toJSON = function () {
    return { id: this._scope, values: this.values.toJSON() };
};

VariableScope.prototype.replaceIn = function (template) {
    if (typeof template !== 'string') { return template; }

    /* pm.variables sees every scope and the {{$dynamic}} generators, so the
     * Go resolver does the work; a single scope substitutes only its own. */
    if (this._chained) { return host.varResolve(template); }

    var obj = this.toObject();

    return template.replace(VAR_REF, function (whole, name) {
        return Object.prototype.hasOwnProperty.call(obj, name) ? String(obj[name]) : whole;
    });
};

VariableScope.prototype.variables = function () { return this.toObject(); };

VariableScope.prototype.syncVariablesTo = function (target) {
    var obj = this.toObject();

    target = target || {};
    Object.keys(target).forEach(function (k) {
        if (!Object.prototype.hasOwnProperty.call(obj, k)) { delete target[k]; }
    });
    Object.keys(obj).forEach(function (k) { target[k] = obj[k]; });

    return target;
};

VariableScope.prototype.syncVariablesFrom = function (source) {
    var self = this;

    source && Object.keys(source).forEach(function (k) { self.set(k, source[k]); });
};

/* A snapshot VariableList view. Postman exposes the live list here; postctl
 * rebuilds it on every read, so mutating it does not write back (see the
 * compatibility table in docs/scripting.md). */
Object.defineProperty(VariableScope.prototype, 'values', {
    enumerable: true,
    get: function () {
        var obj = this.toObject();

        return new NS.VariableList(this, Object.keys(obj).map(function (k) {
            return { key: k, value: obj[k] };
        }));
    }
});

VariableScope.isVariableScope = function (obj) { return obj instanceof VariableScope; };
VariableScope._postman_propertyName = 'VariableScope';
NS.VariableScope = VariableScope;
