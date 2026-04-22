- make sure this setup is finest,lightest.clean everything that is not req.
- remove the old code and files that are not req. anymore.
- agent send the data to server with the token and identity.
- server authenticate the agent with the token and identity and store the data in sqlite.
- server expose an api to get the data.
- create a simple frontend to display the data.
- remove extra functions and files that are not req. anymore.
- make sure the code is clean and well structured.
- clean database table that are not req. anymore.
- make sure the code is well commented and easy to understand.
- make sure the code is well tested and has good test coverage.
- make sure there is no security vulnerabilities in the code.
- make sure project server/agent/frontend are as lightweight and efficient as possible 
- application should be able to handle multiple agents sending data concurrently without any performance issues.expectation is 20 agents sending data every 10 seconds.per agent will have 20 containers running and sending data to server.
- handle proper retries and error handling in case of network issues or server downtime.


agent:
- make sure this setup is finest,lightest.clean everything that is not req.
- remove the old code and files that are not req. anymore.
- agent send the data to server with the token and identity.
- remove extra functions and files that are not req. anymore.
- make sure the code is clean and well structured.
- make sure the code is well commented and easy to understand.
- make sure the code is well tested and has good test coverage.
- make sure there is no security vulnerabilities in the code.
- make sure project agent are as lightweight and efficient as possible 
- handle proper retries and error handling in case of network issues or server downtime.








- Raw (10s) → kept 10min, serves live cards, do i even need 10 min? i just want to show the last 10s in live cards, so maybe i can just keep 1 min of raw data and then aggregate it to 1-min averages and keep that for longer.what do u think?

- 1-min averages → kept N days (env-configurable), serves charts

- then delete old data

what is your suggestion?


--
write a very much detailed README.md for the project, this should mention all the internal pieces of this project, this should mention how every functionality is working,
for agent:
- what is it collecting
- how it is sending data to the server
- how it handles retries and error handling
- how to set up and run the agent
- what unites of data it is sending (e.g. CPU usage, memory usage, etc.)
- how authentication works with token and identity
- mention every detail about the code structure and files in the agent


for server:
- how it receives data from agents
- how it authenticates agents
- how it stores data in sqlite
- how it exposes API to get the data
- how to set up and run the server
- mention every detail about the code structure and files in the server

for frontend:
- how it fetches data from the server
- how it displays the data
- how to set up and run the frontend
- mention every detail about the code structure and files in the frontend

for database:
- how the data is structured in sqlite
- what tables are there and what data they store
- how to set up and manage the database

this 


- add dark mode and light mode toggle in the frontend
- make the frontend responsive and mobile-friendly


on every agent card
- add a alert button
- button to enable/disable alerts for that agent
- when alert is enabled, show a form to set the alert threshold (e.g. CPU usage > 80%) for system metrics (CPU, memory, disk usage, etc.),checkbox to select which metric to monitor, and a field to enter the webhook URL to send the alert to.
- when the threshold is breached, show a notification in the frontend and also send an webhook to a specified URL with the alert details (agent identity, metric, value, timestamp, etc.)
- also checkbox to monitor alert on container metrics (CPU, memory usage, etc.) and set threshold for that as well, also send webhook when breached.
- also send alert is container is in not running state for more than 1 minute. 
- and if agent is unreachable for more than 1 minute, send an alert as well.
- make sure to handle proper retries and error handling in case of network issues or server downtime when sending alerts.