// A script whose assertions are meant to fail, so failure reporting is covered.
pm.test('passing assertion', function () {
    pm.response.to.have.status(200);
});

pm.test('wrong status', function () {
    pm.response.to.have.status(201);
});

pm.test('missing property', function () {
    pm.expect(pm.response.json()).to.have.property('total');
});

pm.test('bad deep equality', function () {
    pm.expect(pm.response.json().items).to.eql([{ sku: 'B-2' }]);
});

pm.test('failing async assertion', function (done) {
    setTimeout(function () {
        try {
            pm.expect(pm.response.responseTime).to.be.above(10000);
            done();
        } catch (e) {
            done(e);
        }
    }, 1);
});
