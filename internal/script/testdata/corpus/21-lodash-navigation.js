// lodash doing the heavy lifting on a nested payload.
const _ = require('lodash');
const body = pm.response.json();

pm.test('lodash reads nested paths', function () {
    pm.expect(_.get(body, 'data.orders[0].lines[1].sku')).to.eql('B-2');
    pm.expect(_.has(body, 'data.orders[1].customer.email')).to.be.true;
    pm.expect(_.get(body, 'data.missing.deep', 'fallback')).to.eql('fallback');
});

pm.test('lodash aggregates', function () {
    const totals = _.map(body.data.orders, function (order) {
        return _.sumBy(order.lines, 'amount');
    });

    pm.expect(totals).to.eql([30, 15]);
    pm.expect(_.sum(totals)).to.eql(45);
    pm.expect(_.maxBy(body.data.orders, 'id').id).to.eql(2);
});

const emails = _.chain(body.data.orders)
    .map('customer.email')
    .compact()
    .sortBy()
    .value();

console.log('emails', emails.join(','));
console.log('grouped', JSON.stringify(_.countBy(body.data.orders, 'status')));
pm.collectionVariables.set('orderIds', _.map(body.data.orders, 'id').join(','));
