// Read the response headers, including a repeated one.
pm.test('content type and caching', function () {
    pm.response.to.have.header('Content-Type');
    pm.response.to.have.header('Content-Type', 'application/json; charset=utf-8');
    pm.expect(pm.response.headers.get('cache-control')).to.eql('no-store');
});

pm.test('header lookup is case insensitive', function () {
    pm.expect(pm.response.headers.has('X-Request-Id')).to.be.true;
    pm.expect(pm.response.headers.get('x-request-id')).to.eql('rq-7');
    pm.expect(pm.response.headers.one('X-REQUEST-ID').value).to.eql('rq-7');
});

pm.environment.set('lastRequestId', pm.response.headers.get('X-Request-Id'));

console.log('count', pm.response.headers.count());
console.log('keys', pm.response.headers.map(function (h) { return h.key; }).join(','));
console.log('object', JSON.stringify(pm.response.headers.toObject()));
