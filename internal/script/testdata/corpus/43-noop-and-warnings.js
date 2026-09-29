// The surfaces postctl accepts but does not act on. None of these may throw.
pm.visualizer.set('<h1>{{title}}</h1>', { title: 'Sales' });

const secret = await pm.vault.get('api-key');

console.log('vault get returns', secret);

pm.test('the script keeps running past the no-ops', function () {
    pm.expect(pm.response.code).to.eql(200);
});

console.log('visualizer and vault did not throw');
