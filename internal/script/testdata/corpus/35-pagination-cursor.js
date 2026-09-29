// Drive a paging loop from the response cursor.
const body = pm.response.json();

pm.test('page is well formed', function () {
    pm.expect(body.items).to.have.lengthOf(2);
    pm.expect(body).to.have.property('nextCursor');
});

const seen = JSON.parse(pm.collectionVariables.get('seenIds') || '[]')
    .concat(body.items.map(function (item) { return item.id; }));

pm.collectionVariables.set('seenIds', JSON.stringify(seen));

if (body.nextCursor) {
    pm.collectionVariables.set('cursor', body.nextCursor);
    pm.execution.setNextRequest('List orders');
    console.log('continuing from', body.nextCursor, 'with', seen.length, 'seen');
} else {
    pm.collectionVariables.unset('cursor');
    pm.execution.setNextRequest(null);
}
