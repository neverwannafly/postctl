// Url accessors and query parameter editing.
console.log('toString', pm.request.url.toString());
console.log('host', pm.request.url.getHost());
console.log('path', pm.request.url.getPath());
console.log('query', pm.request.url.getQueryString());
console.log('remote', pm.request.url.getRemote());
console.log('pathWithQuery', pm.request.url.getPathWithQuery());
console.log('protocol', pm.request.url.protocol, 'port', pm.request.url.port);

pm.request.url.query.upsert({ key: 'page', value: '2' });
pm.request.url.addQueryParams('sort=created');
pm.request.url.removeQueryParams('debug');
pm.request.url.query.add({ key: 'internal', value: 'yes', disabled: true });

console.log('after', pm.request.url.toString());
console.log('params', JSON.stringify(pm.request.url.query.toObject()));
