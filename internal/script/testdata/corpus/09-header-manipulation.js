// Header list editing through the PropertyList API.
pm.request.headers.add({ key: 'X-Added', value: 'one' });
pm.request.headers.upsert({ key: 'Accept', value: 'application/json' });
pm.request.headers.upsert({ key: 'X-Added', value: 'two' });
pm.request.headers.remove('X-Remove-Me');
pm.request.addHeader({ key: 'X-Via-Request', value: 'yes' });
pm.request.removeHeader('X-Legacy');

console.log('count', pm.request.headers.count());
console.log('has accept', pm.request.headers.has('ACCEPT'));
console.log('get added', pm.request.headers.get('x-added'));
console.log('object', JSON.stringify(pm.request.headers.toObject()));
console.log('getHeaders', JSON.stringify(pm.request.getHeaders()));

const keys = [];

pm.request.headers.each(function (header) { keys.push(header.key); });
console.log('keys', keys.join(','));
