// The status-class helpers on a 404.
pm.test('is a client error', function () {
    pm.response.to.be.clientError;
    pm.response.to.have.status(404);
    pm.response.to.have.status('Not Found');
});

pm.test('is not a success or a server error', function () {
    pm.expect(pm.response.code).to.be.within(400, 499);
    pm.expect(pm.response.status).to.eql('Not Found');
    pm.expect(pm.response.responseTime).to.be.below(1000);
});

pm.test('has a problem+json body', function () {
    pm.response.to.have.header('Content-Type');
    pm.response.to.have.jsonBody('detail');
    pm.expect(pm.response.json().status).to.eql(404);
});

console.log('reason', pm.response.reason());
console.log('size', pm.response.responseSize > 0);
