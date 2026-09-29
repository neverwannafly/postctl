// Deep structural assertions over a nested payload.
const body = pm.response.json();

pm.test('the whole object matches', function () {
    pm.expect(body).to.deep.equal({
        order: {
            id: 'ord-9',
            customer: { id: 'cust-1', tags: ['vip', 'eu'] },
            lines: [{ sku: 'A-1', qty: 1 }]
        }
    });
});

pm.test('nested members and shapes', function () {
    pm.expect(body.order).to.have.nested.property('customer.tags[0]', 'vip');
    pm.expect(body.order.customer).to.include({ id: 'cust-1' });
    pm.expect(body.order.lines).to.deep.include({ sku: 'A-1', qty: 1 });
    pm.expect(body.order).to.have.all.keys(['id', 'customer', 'lines']);
});

pm.test('members and ordering', function () {
    pm.expect(body.order.customer.tags).to.have.members(['eu', 'vip']);
    pm.expect(body.order.customer.tags).to.have.ordered.members(['vip', 'eu']);
});
