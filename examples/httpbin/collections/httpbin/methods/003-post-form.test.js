pm.test("Status code is 200", function () {
  pm.response.to.have.status(200);
});

pm.test("Form data was echoed", function () {
  const json = pm.response.json();
  pm.expect(json.form.username).to.eql("postctl");
  pm.expect(json.form.action).to.eql("login");
});
