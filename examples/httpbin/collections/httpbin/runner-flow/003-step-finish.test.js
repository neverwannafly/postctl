pm.test("Step finish reached", function () {
  pm.response.to.have.status(200);
});

// Stop runner execution
pm.execution.setNextRequest(null);
