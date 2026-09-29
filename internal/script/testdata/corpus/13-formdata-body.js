// Inspect and extend a multipart form body.
pm.request.body.formdata.upsert({ key: 'title', value: 'Quarterly report' });
pm.request.body.formdata.add({ key: 'uploadedBy', value: pm.environment.get('operator') });

console.log('mode', pm.request.body.mode);
console.log('count', pm.request.body.formdata.count());
console.log('title', pm.request.body.formdata.get('title'));

const parts = [];

pm.request.body.formdata.each(function (part) {
    parts.push(part.key + ':' + (part.type === 'file' ? part.src : part.value));
});
console.log('parts', parts.join('|'));
