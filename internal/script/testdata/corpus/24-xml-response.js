// A SOAP-ish XML response parsed with xml2js and with the legacy helper.
const xml2js = require('xml2js');

let parsed;

xml2js.parseString(pm.response.text(), { explicitArray: false, trim: true }, function (err, result) {
    if (err) { throw err; }
    parsed = result;
});

pm.test('xml2js parses the envelope', function () {
    pm.expect(parsed.order.id).to.eql('ord-5');
    pm.expect(parsed.order.lines.line).to.have.lengthOf(2);
    pm.expect(parsed.order.lines.line[0].sku).to.eql('A-1');
});

pm.test('legacy xml2Json agrees', function () {
    const legacy = xml2Json(pm.response.text());

    pm.expect(legacy.order.id).to.eql('ord-5');
});

const builder = new xml2js.Builder({ headless: true, renderOpts: { pretty: false } });

console.log('rebuilt', builder.buildObject({ ack: { id: parsed.order.id } }));
