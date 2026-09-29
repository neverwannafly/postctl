// Fan out with Promise.all over pm.sendRequest promises.
const base = pm.environment.get('lookupUrl');

const responses = await Promise.all([
    pm.sendRequest(base + '/regions'),
    pm.sendRequest(base + '/currencies'),
    pm.sendRequest(base + '/locales')
]);

const codes = responses.map(function (res) { return res.code; });
const names = responses.map(function (res) { return res.json().name; });

pm.collectionVariables.set('lookupNames', names.join(','));

console.log('codes', codes.join(','));
console.log('names', names.join(','));
console.log('all ok', responses.every(function (res) { return res.code === 200; }));
