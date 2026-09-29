// Correlation identifiers. v5 is deterministic, v4 is only checked structurally.
const uuid = require('uuid');

const correlationId = uuid.v5(pm.info.requestName, uuid.v5.DNS);
const requestId = uuid.v4();

pm.request.headers.upsert({ key: 'X-Correlation-Id', value: correlationId });
pm.collectionVariables.set('correlationId', correlationId);

console.log('v5 is stable', correlationId);
console.log('v5 matches a second call', uuid.v5(pm.info.requestName, uuid.v5.DNS) === correlationId);
console.log('v4 looks like a uuid', uuid.validate(requestId), uuid.version(requestId));
console.log('v4 values differ', uuid.v4() !== requestId);
console.log('crypto.randomUUID', uuid.validate(crypto.randomUUID()));
