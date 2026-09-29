// Date windows computed with moment, as reporting collections do.
const moment = require('moment');

const anchor = moment.utc(pm.environment.get('anchorDate'));

pm.environment.set('windowStart', anchor.clone().subtract(7, 'days').format('YYYY-MM-DD'));
pm.environment.set('windowEnd', anchor.clone().format('YYYY-MM-DD'));
pm.environment.set('isoTimestamp', anchor.clone().toISOString());
pm.environment.set('epochSeconds', anchor.clone().unix());

pm.request.url.query.upsert({ key: 'from', value: pm.environment.get('windowStart') });
pm.request.url.query.upsert({ key: 'to', value: pm.environment.get('windowEnd') });

console.log('window', pm.environment.get('windowStart'), '->', pm.environment.get('windowEnd'));
console.log('weekday', anchor.format('dddd'), 'quarter', anchor.quarter());
