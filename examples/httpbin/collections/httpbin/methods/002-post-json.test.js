pm.test("Status code is 200", function () {
  pm.response.to.have.status(200);
});

pm.test("JSON payload was echoed", function () {
  const json = pm.response.json();
  pm.expect(json.json.name).to.eql("postctl");
  pm.expect(json.json.type).to.eql("terminal-ui");
});
