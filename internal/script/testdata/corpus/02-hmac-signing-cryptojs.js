// Sign the request the way a payments gateway usually wants it.
const CryptoJS = require('crypto-js');

const timestamp = pm.environment.get('fixedTimestamp');
const nonce = pm.environment.get('fixedNonce');
const secret = pm.environment.get('signingSecret');

const canonical = [
    pm.request.method.toUpperCase(),
    pm.request.url.getPath(),
    pm.request.url.getQueryString(),
    timestamp,
    nonce
].join('\n');

const signature = CryptoJS.HmacSHA256(canonical, secret).toString(CryptoJS.enc.Base64);

pm.request.headers.upsert({ key: 'X-Timestamp', value: timestamp });
pm.request.headers.upsert({ key: 'X-Nonce', value: nonce });
pm.request.headers.upsert({ key: 'X-Signature', value: signature });

pm.variables.set('canonicalString', canonical);
console.log('signature', signature);
