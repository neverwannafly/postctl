// Edit a urlencoded form body.
pm.request.body.urlencoded.upsert({ key: 'grant_type', value: 'refresh_token' });
pm.request.body.urlencoded.upsert({ key: 'refresh_token', value: pm.environment.get('refreshToken') });
pm.request.body.urlencoded.add({ key: 'scope', value: 'read write' });
pm.request.body.urlencoded.remove('unused');

console.log('mode', pm.request.body.mode);
console.log('count', pm.request.body.urlencoded.count());
console.log('object', JSON.stringify(pm.request.body.urlencoded.toObject()));
console.log('encoded', pm.request.body.toString());
