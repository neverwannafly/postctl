// The pm.response.to.* family from chai-postman.
pm.test('status by code and by reason', function () {
    pm.response.to.have.status(200);
    pm.response.to.have.status('OK');
    pm.response.to.not.have.status(404);
});

pm.test('status classes', function () {
    pm.response.to.be.ok;
    pm.response.to.be.success;
    pm.response.to.not.be.error;
    pm.response.to.not.be.clientError;
    pm.response.to.not.be.serverError;
    pm.response.to.have.statusCodeClass(2);
});

pm.test('headers', function () {
    pm.response.to.have.header('Content-Type');
    pm.response.to.have.header('content-type', 'application/json');
    pm.response.to.not.have.header('X-Absent');
    pm.expect(pm.response.headers.get('X-Request-Id')).to.eql('rid-7');
});

pm.test('body shapes', function () {
    pm.response.to.be.json;
    pm.response.to.have.jsonBody();
    pm.response.to.have.jsonBody('data.id');
    pm.response.to.have.jsonBody('data.id', 7);
    pm.response.to.have.body(/"id":\s*7/);
    pm.response.to.have.jsonBody({ data: { id: 7, label: 'seven' } });
});

pm.test('timing and size', function () {
    pm.expect(pm.response.responseTime).to.be.a('number').and.to.be.below(1000);
    pm.expect(pm.response.responseSize).to.be.a('number').and.to.be.above(0);
    pm.expect(pm.response.code).to.eql(200);
    pm.expect(pm.response.status).to.eql('OK');
});
