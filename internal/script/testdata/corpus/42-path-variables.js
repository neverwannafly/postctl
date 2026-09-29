// Fill :path variables and add a trailing segment.
console.log('before', pm.request.url.toString());
console.log('variables', pm.request.url.variables.count());

pm.request.url.variables.each(function (v) { console.log('var', v.key, v.value); });

pm.request.url.variables.upsert({ key: 'orderId', value: pm.collectionVariables.get('lastOrderId') });
pm.request.url.variables.upsert({ key: 'lineId', value: '3' });
pm.request.url.path.push('receipt');

console.log('after', pm.request.url.toString());
console.log('resolved path', pm.request.url.getPath());
console.log('raw path', pm.request.url.getPath(true));
