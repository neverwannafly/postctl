// The chai chains Postman users lean on most.
const body = pm.response.json();

pm.test('array chains', function () {
    pm.expect(body.tags).to.be.an('array').that.is.not.empty;
    pm.expect(body.tags).to.have.lengthOf(3);
    pm.expect(body.tags).to.include('beta');
    pm.expect(body.tags).to.have.members(['alpha', 'beta', 'gamma']);
    pm.expect(body.tags).to.deep.equal(['alpha', 'beta', 'gamma']);
});

pm.test('object chains', function () {
    pm.expect(body).to.have.property('meta');
    pm.expect(body.meta).to.have.all.keys('page', 'total');
    pm.expect(body.meta).to.deep.include({ page: 1 });
    pm.expect(body.meta.total).to.be.a('number').that.is.above(0);
    pm.expect(body.meta.total).to.be.within(1, 100);
});

pm.test('string and negation chains', function () {
    pm.expect(body.name).to.be.a('string').and.to.match(/^ord-/);
    pm.expect(body.name).to.have.string('ord');
    pm.expect(body.missing).to.be.undefined;
    pm.expect(body.nothing).to.be.null;
    pm.expect(body.name).to.not.equal('other');
    pm.expect(body.flag).to.be.true;
});

pm.test('custom failure message', function () {
    pm.expect(body.meta.page, 'page should be the first one').to.eql(1);
});
