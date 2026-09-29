// Every scope, and the precedence between them.
console.log('globals', pm.globals.get('tier'), 'collection', pm.collectionVariables.get('tier'));
console.log('environment', pm.environment.get('tier'));
console.log('merged wins', pm.variables.get('tier'));
console.log('only in globals', pm.variables.get('region'));

pm.variables.set('runScoped', 'local-only');
console.log('local', pm.variables.get('runScoped'), pm.environment.has('runScoped'));

pm.collectionVariables.set('lastRun', '2024-03-15');
pm.globals.unset('region');
console.log('after unset', pm.globals.has('region'), pm.variables.get('region'));

console.log('env object', JSON.stringify(pm.environment.toObject()));
console.log('collection object', JSON.stringify(pm.collectionVariables.toObject()));
