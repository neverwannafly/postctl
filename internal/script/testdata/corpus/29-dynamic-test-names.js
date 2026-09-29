// One test per row of the payload, a very common runner idiom.
const body = pm.response.json();

pm.test('the list is not empty', function () {
    pm.expect(body.items).to.be.an('array').that.is.not.empty;
});

body.items.forEach(function (item, index) {
    pm.test('item ' + index + ' (' + item.sku + ') is well formed', function () {
        pm.expect(item).to.have.all.keys('sku', 'qty', 'price');
        pm.expect(item.qty).to.be.a('number').and.to.be.above(0);
        pm.expect(item.price).to.be.at.least(0);
    });
});

pm.test('totals add up', function () {
    const total = body.items.reduce(function (sum, item) { return sum + item.qty * item.price; }, 0);

    pm.expect(total).to.eql(body.total);
});
