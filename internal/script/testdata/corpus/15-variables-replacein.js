// Template substitution from a script.
console.log('chained', pm.variables.replaceIn('{{scheme}}://{{host}}/{{version}}/orders'));
console.log('unknown stays', pm.variables.replaceIn('{{host}}/{{notDefined}}'));
console.log('environment only', pm.environment.replaceIn('{{host}}|{{version}}'));
console.log('dynamic', pm.variables.replaceIn('ts={{$timestamp}}'));

pm.request.url.query.upsert({
    key: 'callback',
    value: pm.variables.replaceIn('{{scheme}}://{{host}}/callback')
});
console.log('url', pm.request.url.toString());
