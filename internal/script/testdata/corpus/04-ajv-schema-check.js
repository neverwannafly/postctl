// JSON Schema validation with ajv, both directly and through pm.expect.
const Ajv = require('ajv');

const schema = {
    type: 'object',
    required: ['id', 'status', 'items'],
    properties: {
        id: { type: 'string' },
        status: { type: 'string', enum: ['open', 'closed'] },
        items: {
            type: 'array',
            minItems: 1,
            items: {
                type: 'object',
                required: ['sku', 'qty'],
                properties: { sku: { type: 'string' }, qty: { type: 'integer', minimum: 1 } }
            }
        }
    }
};

pm.test('response matches the order schema', function () {
    const ajv = new Ajv({ allErrors: true });
    const valid = ajv.validate(schema, pm.response.json());

    if (!valid) { console.error('schema errors:', ajv.errorsText()); }
    pm.expect(valid, ajv.errorsText()).to.be.true;
});

pm.test('chai-postman jsonSchema assertion agrees', function () {
    pm.response.to.have.jsonSchema(schema);
});

pm.test('a bad payload is rejected', function () {
    const ajv = new Ajv({ allErrors: true });

    pm.expect(ajv.validate(schema, { id: 'x', status: 'unknown', items: [] })).to.be.false;
});
