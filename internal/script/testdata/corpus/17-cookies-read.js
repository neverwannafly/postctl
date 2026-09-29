// Read the cookies that apply to the request.
pm.test('session cookie is present', function () {
    pm.expect(pm.cookies.has('session')).to.be.true;
    pm.expect(pm.cookies.get('session')).to.eql('sess-1');
});

pm.test('cookie attributes are visible', function () {
    const session = pm.cookies.one('session');

    pm.expect(session.httpOnly).to.be.true;
    pm.expect(session.secure).to.be.true;
    pm.expect(session.path).to.eql('/');
});

pm.test('the response carried its own Set-Cookie', function () {
    pm.expect(pm.response.cookies.get('session')).to.eql('sess-2');
});

console.log('jar', JSON.stringify(pm.cookies.toObject()));
console.log('count', pm.cookies.count());
console.log('from the response', JSON.stringify(responseCookies));
