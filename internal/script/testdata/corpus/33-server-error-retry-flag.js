// A 500 flips a retry flag and stops the run.
pm.test('is a server error', function () {
    pm.response.to.be.serverError;
    pm.expect(pm.response.to.be.error).to.not.be.undefined;
});

pm.test('is neither ok nor success', function () {
    pm.expect(pm.response.code).to.eql(500);
    pm.expect(function () { pm.response.to.be.ok; }).to.throw();
    pm.expect(function () { pm.response.to.be.success; }).to.throw();
});

const attempts = Number(pm.collectionVariables.get('attempts') || 0) + 1;

pm.collectionVariables.set('attempts', attempts);

if (attempts >= 3) {
    console.warn('giving up after', attempts, 'attempts');
    pm.execution.setNextRequest(null);
} else {
    pm.execution.setNextRequest(pm.info.requestName);
}
