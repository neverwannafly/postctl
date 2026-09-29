pm.test("Status code is 200", function () {
  pm.response.to.have.status(200);
});

pm.test("Basic auth succeeded", function () {
  const json = pm.response.json();
  pm.expect(json.authenticated).to.be.true;
  pm.expect(json.user).to.eql("postctl");
});
