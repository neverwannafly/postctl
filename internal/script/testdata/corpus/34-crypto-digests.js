// The crypto-js digests scripts reach for most often.
const CryptoJS = require('crypto-js');

const payload = pm.request.body.raw;
const key = pm.environment.get('signingKey');

const md5 = CryptoJS.MD5(payload).toString();
const sha1 = CryptoJS.SHA1(payload).toString();
const sha256 = CryptoJS.SHA256(payload).toString(CryptoJS.enc.Hex);
const sha512 = CryptoJS.SHA512(payload).toString();
const hmac = CryptoJS.HmacSHA1(payload, key).toString();

const encrypted = CryptoJS.AES.encrypt(payload, key).toString();
const decrypted = CryptoJS.AES.decrypt(encrypted, key).toString(CryptoJS.enc.Utf8);

pm.request.headers.upsert({ key: 'Content-MD5', value: md5 });
pm.request.headers.upsert({ key: 'X-Content-Sha256', value: sha256 });

console.log('md5', md5);
console.log('sha1', sha1);
console.log('sha512 prefix', sha512.slice(0, 32));
console.log('hmac-sha1', hmac);
console.log('aes round trip', decrypted === payload);
