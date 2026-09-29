// Poll until the job reports ready, with the event loop driving the waits.
pm.test('polls until the job is ready', function (done) {
    let attempts = 0;

    function poll() {
        attempts += 1;
        pm.sendRequest(pm.environment.get('statusUrl'), function (err, res) {
            if (err) { done(err); return; }

            const state = res.json().state;

            console.log('attempt', attempts, 'state', state);

            if (state !== 'ready') { setTimeout(poll, 2); return; }

            pm.expect(res.code).to.eql(200);
            pm.expect(attempts).to.eql(3);
            pm.expect(ticks, 'the interval stopped itself').to.eql(2);
            done();
        });
    }

    setTimeout(poll, 2);
});

// A self-clearing interval, the other timer idiom scripts use.
let ticks = 0;
const ticker = setInterval(function () {
    ticks += 1;
    if (ticks === 2) { clearInterval(ticker); }
}, 1);
