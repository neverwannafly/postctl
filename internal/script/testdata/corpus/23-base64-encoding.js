// Base64 three ways: btoa, crypto-js and Buffer.
const CryptoJS = require('crypto-js');

const user = pm.environment.get('basicUser');
const pass = pm.environment.get('basicPass');
const pair = user + ':' + pass;

const viaBtoa = btoa(pair);
const viaCryptoJs = CryptoJS.enc.Base64.stringify(CryptoJS.enc.Utf8.parse(pair));
const viaBuffer = Buffer.from(pair, 'utf8').toString('base64');

pm.request.headers.upsert({ key: 'Authorization', value: 'Basic ' + viaBtoa });

console.log('all three agree', viaBtoa === viaCryptoJs && viaCryptoJs === viaBuffer);
console.log('encoded', viaBtoa);
console.log('round trip', atob(viaBtoa) === pair);
console.log('url safe', CryptoJS.enc.Base64.stringify(CryptoJS.enc.Utf8.parse('a?b/c')).replace(/\+/g, '-'));
