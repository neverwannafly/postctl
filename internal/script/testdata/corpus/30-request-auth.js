// Swap bearer auth for an API key at request time.
console.log('inherited type', pm.request.auth.type);
console.log('inherited token', pm.request.auth.parameters().get('token'));

pm.request.auth.update({ key: 'X-Api-Key', value: pm.environment.get('apiKey'), in: 'header' }, 'apikey');

console.log('new type', pm.request.auth.type);
console.log('new key', pm.request.auth.parameters().get('key'));
console.log('new value', pm.request.auth.parameters().get('value'));
console.log('current', pm.request.auth.current());
