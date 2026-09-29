// A reporting window computed with moment, in UTC so the result is stable.
const moment = require('moment');

const anchor = moment.utc(pm.environment.get('anchorDate'));
const from = anchor.clone().startOf('month');
const to = anchor.clone().endOf('month');

pm.request.url.query.upsert({ key: 'from', value: from.format('YYYY-MM-DD') });
pm.request.url.query.upsert({ key: 'to', value: to.format('YYYY-MM-DD') });
pm.request.url.query.upsert({ key: 'generatedAt', value: anchor.toISOString() });

pm.environment.set('reportWindow', from.format('YYYY-MM') );

console.log('window', from.format('YYYY-MM-DD'), 'to', to.format('YYYY-MM-DD'));
console.log('days in month', anchor.daysInMonth());
console.log('week of year', anchor.isoWeek());
console.log('relative', anchor.clone().add(7, 'days').diff(anchor, 'hours'), 'hours');
console.log('formatted', anchor.format('dddd, D MMMM YYYY HH:mm:ss [UTC]'));
