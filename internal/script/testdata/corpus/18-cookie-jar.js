// Programmatic cookie jar, callback form then promise form.
const jar = pm.cookies.jar();
const url = 'https://api.example.com/v1/orders';

jar.set(url, 'csrf', 'tok-55', function (err) {
    if (err) { console.error(err.message); return; }
    jar.get(url, 'csrf', function (getErr, value) {
        console.log('callback get', value);
    });
});

pm.test('promise form round-trips', async function () {
    await jar.set(url, { name: 'consent', value: 'granted' });
    const all = await jar.getAll(url);

    pm.expect(all.count()).to.eql(3);
    pm.expect(all.get('consent')).to.eql('granted');

    await jar.unset(url, 'locale');
    pm.expect((await jar.getAll(url)).has('locale')).to.be.false;
});
