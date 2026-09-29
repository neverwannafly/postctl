// The postman.* variable helpers, still common in older collections.
postman.setEnvironmentVariable('stage', 'two');
postman.setGlobalVariable('runId', 'run-1');

console.log('env', postman.getEnvironmentVariable('stage'));
console.log('global', postman.getGlobalVariable('runId'));
console.log('seeded', postman.getEnvironmentVariable('seeded'));

postman.clearEnvironmentVariable('seeded');
console.log('after clear', pm.environment.has('seeded'));

console.log('legacy request', request.method, request.url);
console.log('legacy headers', typeof request.headers, request.headers.Accept);
