// Skip a request that this environment does not have a licence for.
const features = (pm.environment.get('features') || '').split(',');

if (features.indexOf('billing') === -1) {
    console.warn('billing is not enabled here, skipping', pm.info.requestName);
    pm.execution.skipRequest();
} else {
    pm.request.headers.upsert({ key: 'X-Feature', value: 'billing' });
}

console.log('features', features.join('|'));
