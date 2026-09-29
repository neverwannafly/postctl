pm.test("Status code is 200", function () {
  pm.response.to.have.status(200);
});

pm.test("Multipart fields and files received", function () {
  const json = pm.response.json();
  pm.expect(json.form.description).to.eql("sample multipart upload");
  pm.expect(json.files).to.have.property("file");
});
