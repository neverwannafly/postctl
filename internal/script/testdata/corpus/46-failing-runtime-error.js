// A typo in a test script: the run must report it with a line, not crash.
pm.test('this one runs', function () {
    pm.expect(pm.response.code).to.eql(200);
});

const body = pm.response.json();

pm.environment.set('total', body.summary.total);

pm.test('this one never registers', function () {
    pm.expect(true).to.be.true;
});
