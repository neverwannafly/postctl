pm.test("Status code is 200", function () {
  pm.response.to.have.status(200);
});

pm.test("Bearer auth succeeded", function () {
  const json = pm.response.json();
  pm.expect(json.authenticated).to.be.true;
  pm.expect(json.token).to.be.a("string");
});
