pm.test("Step start executed", function () {
  pm.response.to.have.status(200);
});

// Jump over step-skip straight to step-finish
pm.execution.setNextRequest("Step finish");
