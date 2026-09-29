// A pre-pm era test script.
var jsonData = JSON.parse(responseBody);

tests['Status code is 200'] = responseCode.code === 200;
tests['Status name is OK'] = responseCode.name === 'OK';
tests['Body has an id'] = jsonData.id !== undefined;
tests['Response is fast'] = responseTime < 1000;
tests['Content-Type is json'] = responseHeaders['content-type'].indexOf('json') > -1;
tests['Header helper agrees'] = postman.getResponseHeader('Content-Type').indexOf('json') > -1;

postman.setEnvironmentVariable('lastOrderId', jsonData.id);
postman.setGlobalVariable('lastRunStatus', responseCode.name);
