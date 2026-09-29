pm.test("Status code is 200", function () {
  pm.response.to.have.status(200);
});

pm.test("HMAC headers sent and echoed", function () {
  const json = pm.response.json();
  pm.expect(json.headers).to.have.property('X-Timestamp');
  pm.expect(json.headers).to.have.property('X-Signature');
});
