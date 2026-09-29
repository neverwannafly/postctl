// Rewrite a raw JSON body before sending.
const body = JSON.parse(pm.request.body.raw);

body.requestId = pm.environment.get('fixedRequestId');
body.items = body.items.map(function (item) {
    item.total = item.qty * item.unitPrice;

    return item;
});
body.grandTotal = body.items.reduce(function (sum, item) { return sum + item.total; }, 0);

pm.request.body.raw = JSON.stringify(body);
pm.request.headers.upsert({ key: 'Content-Type', value: 'application/json' });

console.log('mode', pm.request.body.mode, 'total', body.grandTotal);
console.log('isEmpty', pm.request.body.isEmpty());
