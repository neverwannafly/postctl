// Schema validation both ways scripts do it: pm.response.to.have.jsonSchema and tv4.
const schema = {
    type: 'object',
    required: ['id', 'status', 'amount'],
    properties: {
        id: { type: 'string', pattern: '^ord-' },
        status: { type: 'string', enum: ['open', 'closed'] },
        amount: { type: 'number', minimum: 0 },
        tags: { type: 'array', items: { type: 'string' } }
    }
};

pm.test('matches the schema through chai', function () {
    pm.response.to.have.jsonSchema(schema);
    pm.expect(pm.response.json()).to.be.jsonSchema(schema);
});

pm.test('matches the schema through tv4', function () {
    const tv4 = require('tv4');

    pm.expect(tv4.validate(pm.response.json(), schema), tv4.error && tv4.error.message).to.be.true;
});

pm.test('rejects a bad document', function () {
    const bad = { id: 'x-1', status: 'unknown', amount: -5 };

    pm.expect(require('tv4').validate(bad, schema)).to.be.false;
});
