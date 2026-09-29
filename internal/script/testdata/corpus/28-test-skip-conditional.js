// Skip assertions that do not apply to this environment.
const isProduction = pm.environment.get('envName') === 'production';

pm.test('status is 200', function () {
    pm.response.to.have.status(200);
});

if (isProduction) {
    pm.test('debug payload is absent', function () {
        pm.expect(pm.response.json().debug).to.be.undefined;
    });
} else {
    pm.test.skip('debug payload is absent');
}

pm.test('records the environment', function () {
    pm.expect(pm.environment.get('envName')).to.be.a('string');
});

pm.test.skip('load test only');

console.log('tests registered', pm.test.index());
