// A gateway returned HTML, and the script has to survive it.
pm.test('the response is not JSON', function () {
    let threw = false;

    try {
        pm.response.json();
    } catch (e) {
        threw = true;
        pm.expect(e.name).to.eql('JSONError');
    }

    pm.expect(threw, 'json() should throw on an HTML body').to.be.true;
});

pm.test('the text body is still readable', function () {
    pm.expect(pm.response.text()).to.include('<title>502 Bad Gateway</title>');
    pm.response.to.have.body(/Bad Gateway/);
});

pm.test('the failure is reported upstream', function () {
    pm.expect(pm.response.code).to.eql(502);
    pm.expect(pm.response.to.be.error).to.not.throw;
});

pm.collectionVariables.set('lastFailureReason', pm.response.reason());
