const CryptoJS = require('crypto-js');

const timestamp = Math.floor(Date.now() / 1000).toString();
const secret = pm.environment.get('hmacSecret') || 'default-secret-key';
const method = pm.request.method.toUpperCase();
const path = pm.request.url.getPath();
const message = `${method}:${path}:${timestamp}`;
const hmac = CryptoJS.HmacSHA256(message, secret).toString(CryptoJS.enc.Hex);

pm.request.headers.upsert({ key: 'X-Timestamp', value: timestamp });
pm.request.headers.upsert({ key: 'X-Signature', value: hmac });
