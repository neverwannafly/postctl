pm.test("Status code is 200", function () {
  pm.response.to.have.status(200);
});

pm.test("Query parameter was echoed", function () {
  const json = pm.response.json();
  pm.expect(json.args.filter).to.eql("active");
});
