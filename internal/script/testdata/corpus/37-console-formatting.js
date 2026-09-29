// Every console level and the value shapes scripts log.
console.log('plain string');
console.log('multiple', 1, true, null, undefined);
console.info('object', { a: 1, b: [1, 2], c: { d: 'e' } });
console.warn('array', [1, 'two', { three: 3 }]);
console.error('error', new Error('boom').message);
console.debug('nested', { level1: { level2: { level3: { level4: 'deep' } } } });

const circular = { name: 'root' };

circular.self = circular;
console.log('circular', circular);
console.log('printf style: %s scored %d', 'alice', 42);
