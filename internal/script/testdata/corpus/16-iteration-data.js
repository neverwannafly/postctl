// A collection-runner iteration reading its data-file row.
console.log('iteration', pm.info.iteration, 'of', pm.info.iterationCount);
console.log('row', pm.iterationData.get('customerId'), pm.iterationData.get('amount'));
console.log('object', JSON.stringify(pm.iterationData.toObject()));
console.log('visible through pm.variables', pm.variables.get('customerId'));
console.log('legacy data global', data.customerId);

pm.request.url.query.upsert({ key: 'customer', value: String(pm.iterationData.get('customerId')) });
