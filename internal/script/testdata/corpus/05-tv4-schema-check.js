// Older collections validate with tv4 instead of ajv.
const tv4 = require('tv4');

const schema = {
    type: 'object',
    required: ['users'],
    properties: {
        users: {
            type: 'array',
            items: {
                type: 'object',
                required: ['id', 'email'],
                properties: { id: { type: 'number' }, email: { type: 'string' } }
            }
        }
    }
};

const body = pm.response.json();

tests['Schema is valid'] = tv4.validate(body, schema);

pm.test('tv4 reports no error', function () {
    const result = tv4.validateResult(body, schema);

    pm.expect(result.valid).to.be.true;
    pm.expect(result.error).to.be.null;
});

pm.test('tv4 catches a missing property', function () {
    pm.expect(tv4.validate({ users: [{ id: 1 }] }, schema)).to.be.false;
    pm.expect(tv4.error.message).to.be.a('string');
});
