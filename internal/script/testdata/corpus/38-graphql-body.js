// Fill in the variables of a GraphQL operation.
const variables = JSON.parse(pm.request.body.graphql.variables || '{}');

variables.first = 25;
variables.after = pm.collectionVariables.get('cursor') || null;
variables.tenant = pm.environment.get('tenantId');

pm.request.body.graphql.variables = JSON.stringify(variables);

console.log('mode', pm.request.body.mode);
console.log('query starts', pm.request.body.graphql.query.split('\n')[0]);
console.log('variables', pm.request.body.graphql.variables);
