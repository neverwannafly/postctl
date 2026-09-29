// Branch the collection run on the response, the way polling flows do.
const body = pm.response.json();

pm.test('status is accepted', function () {
    pm.response.to.have.status(202);
});

const attempts = Number(pm.collectionVariables.get('pollAttempts') || 0) + 1;

pm.collectionVariables.set('pollAttempts', attempts);

if (body.state === 'pending' && attempts < 5) {
    console.log('still pending, retrying (attempt ' + attempts + ')');
    pm.execution.setNextRequest('Poll job status');
} else if (body.state === 'pending') {
    console.warn('giving up after ' + attempts + ' attempts');
    pm.execution.setNextRequest(null);
} else {
    pm.execution.setNextRequest('Fetch job result');
}
