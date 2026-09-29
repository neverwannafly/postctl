// Fetch a token from the auth service and attach it to the outgoing request.
const clientId = pm.environment.get('clientId');

pm.sendRequest({
    url: pm.environment.get('authUrl') + '/oauth/token',
    method: 'POST',
    header: {
        'Content-Type': 'application/json',
        Accept: 'application/json'
    },
    body: {
        mode: 'raw',
        raw: JSON.stringify({ client_id: clientId, grant_type: 'client_credentials' })
    }
}, function (err, res) {
    if (err) {
        console.error('token request failed:', err.message);
        return;
    }

    const body = res.json();

    pm.environment.set('accessToken', body.access_token);
    pm.environment.set('tokenExpiresIn', body.expires_in);
    pm.request.headers.upsert({ key: 'Authorization', value: 'Bearer ' + body.access_token });
    console.log('token acquired, expires in', body.expires_in);
});
