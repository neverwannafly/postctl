// Two dependent calls with top-level await, as newer collections write them.
const loginResponse = await pm.sendRequest({
    url: pm.environment.get('authUrl') + '/login',
    method: 'POST',
    header: { 'Content-Type': 'application/json' },
    body: { mode: 'raw', raw: JSON.stringify({ user: pm.environment.get('user') }) }
});

const token = loginResponse.json().token;

pm.environment.set('token', token);

const profileResponse = await pm.sendRequest({
    url: pm.environment.get('authUrl') + '/me',
    header: { Authorization: 'Bearer ' + token }
});

const profile = profileResponse.json();

pm.environment.set('tenantId', profile.tenantId);
pm.request.headers.upsert({ key: 'Authorization', value: 'Bearer ' + token });
pm.request.headers.upsert({ key: 'X-Tenant', value: profile.tenantId });

console.log('login', loginResponse.code, 'profile', profileResponse.code);
console.log('tenant', profile.tenantId);
