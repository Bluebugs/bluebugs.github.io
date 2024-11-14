+++
date = '2024-11-09T14:20:32-07:00'
title = 'Tests Debt'
featured_image = 'images/kicking-horse.jpg'
description = 'Avoiding Tech debt in your tests!'
+++

Tests should help you release code faster and with confidence. Yet, for many developers, testing has the opposite effect, creating delays and frustration. Here, I'll explore common pitfalls in testing and suggest better practices to make tests truly beneficial.

We have all heard that we need to have more tests and that we should have as close to 100% tests coverage as possible. Despite this effort, we still encounter bugs. We still do manual testing and overall a lot of developers do not trust their tests to actually catch anything useful. Why is that?

# Identifying _Technical Debt_ through its symptoms

Diagnosing technical debt often starts by observing its symptoms, such as missed deadlines or delays in delivering new features. Increase in bugs and instability faced by the users of the software is also another symptom. The more _technical debt_ there is in your code, the harder it is to predict the outcome of a planned code change. This impacts both the quality of the result and when that result is available.

The easy answer from management when they see this kind of problem is to push the silver bullet aka more tests and code coverage! Of course this won't pay down any of the existing _technical debt_, but at least next release quality should be good... right?

# Bugs and unpredictable releases

And still, this doesn't solve the problem! In fact, it likely make things worth. Every line of code that is written will have to live for a decade if it is part of any successful software. This means every line of code that is added to a project has the potential to add _technical debt_. This includes TESTS too!

We were told that tests would bring us peace and enable us to confidently release software. How is that not working? Consider what your tests are actually validating for the specific code you're working with. Most of us, software developers, are actually not writing any complex algorithm, but just gluing a bunch of different library, executable and online services to provide new functionality. What most of us do, is write code that integrate things together. This can't be tested properly with unit test.

Unit tests are useful for verifying algorithms or specific functions, like ensuring a sorting function orders items correctly. But when testing code with a lot of dependencies, unit test are not the solution. Why would a mock of your dependencies actually test anything? It duplicate the assumption you had when writing your code. This duplication of assumption is the core of your _technical debt_ in your tests. When you will discover a bug, you will have to change both your code and your test. Your test are not testing anything, they are preventing you from changing your code.

> Note: At least, when you use mock, you can rely on code generation and not pay the cost of writing or maintaining those line. There will never be a good reason to write mock or stubs by hands.

This is the textbook definition of _technical debt_. Something that make you slow at doing any change without providing any benefit.

# Better tests

Understanding how technical debt accumulates in tests is just the beginning. Now, let's explore better testing practices that can reduce this debt and enhance test reliability.

The first step is to recognize when your tests are enforcing belief instead of testing it. And the easiest step for most of us to solve this problem is to not write unit test. We are integrating things together, we should write integration tests and end to end tests, not unit test. Another regular mistake is in mocking dependencies in unit tests, as it often recreates the same assumptions that went into the original code. This leads to duplicated assumptions that accumulate technical debt. For example, mocking a database may hide real-world issues, which can cause bugs to surface later in production. So stop writing unit test!

We have amazing tools today to write this. First a bit of difference between integration tests and end to end tests.
- **Integration tests**: Run before deployment and ensure each module works with its dependencies.
- **End to end tests**: Verify that your code functions as expected in the production environment.

For integration testing, we have a lot of tools that help. You can now spin all your dependencies using [testcontainer](https://testcontainers.com/) with [docker](https://www.docker.com/). For example, you could use testcontainer to set up a temporary PostgreSQL database with Docker, allowing tests to interact with a real database instead of mocks. This ensures your tests behave more like they would in production. Even your laptop is fast enough to spin up a database server, a cache service and let a few tests run against it. If you believe this is going to slow you down, switch to use something like SQLite for your local testing and [Turso](https://turso.tech/) for your database.

Still sometimes, you need to use some SaaS service. Prefer the one that have an open source implementation that allow you to actually do your integration tests without a mock. For example, even if you use [Tailscale](https://tailscale.com/) for your connection between services, you can use [Headscale](https://github.com/juanfont/headscale) in combination with testcontainer for your integration tests.

Some of you might need to connect to some appliance and again that might have been something the first answer was, let's use mock to emulate this device. Still this might not be necessary the best answer. Consider that for [Mikrotik Router](https://mikrotik.com/), you can actually run their OS in a [container](https://github.com/EvilFreelancer/docker-routeros) and tests your service against it! If it is possible for router, whatever your appliance, you should really get it running in the "cloud" if just for better testing!

# Step up your tests

While integration tests improve test coverage, they might not fully simulate the production environment. End-to-end testing fills this gap.

Integration tests are great, but they don't actually tests the final service as it is running in production. And your integration tests, might miss things for that reason. A solution to this problem is to deploy your application in your production environment. Tests it there. And when all tests pass, switch the traffic to the newly deployed application. This is the basis for an end to end test.

This is easy for web application, and you should have no excuse to not already have it. With tool like [cypress](https://www.cypress.io/) or [playwright](https://playwright.dev/), you can easily run tests against your application, check the result and assess if everything looks good as if a human was going through it. Actually this is a lot better than any manual testing as it will not miss any details nor forget to tests some old feature. As you move forward your tests would only grow and whatever the size of your application, your tests will scale.

It is slightly harder when doing native application and require building or using infrastructure that allows for on devices testing. Some OS are harder than other, some are easier. Windows and Linux are the easiest to work with. While iOS might be the hardest. Never the less, if you want to be able to deploy your application to this OS and you want to be confident that you are not breaking anything, invest the time to build that infrastructure. For anyone doing native application, keep in mind the explosion of possibility. Let say you support Windows, Mac, Linux, Android and iOS. That's 5 OS. You then support x86_64 and ARM64. That 2 architectures. Now multiply the manual testing of your application 10 times for each features.

# Drinking your own champagne

If there is one space which is guaranteed to never scale with manual testing, it is native application, but even a web application will not necessarily behave the same on Chrome, Firefox and Safari. So every time you do some scenario manually, take the necessary 30min to write the equivalent integration or end to end tests. Next time you add a feature, this tests will ensure that you still have all your old features working.

Manual testing is something that developer feel good about, as they see the result of their work, but it just work on their computer and doesn't lead to any repeatable result. Also developers can be blind to bugs and issues they get accustomed to.

The solution to this, is **to drink your own champagne** (Formerly __eat your own dog food__, but definitively champagne is better and I am french, so let's go with Champagne).

Drinking your own champagne means using your application as it exists in its current form, similar to a beta program. Deploying daily to a shared environment allows developers and selected users to spot issues early and provide feedback. This iterative approach improves quality and stability with each release.

If you have good integration tests and end to end tests, this beta program should be always in a usable state. With the ability for the non developers user to report and ask improvement ahead of a release to a larger base, this is a critical improvement to have quality release.

# Conclusion

In summary, to release with confidence:
- Shift focus from unit to integration tests.
- Implement end-to-end tests to simulate real-world use.
- Regularly use the product as it’s being built.

Implementing these practices requires some effort, but it will steadily reduce technical debt in your testing process, making your software more reliable and easier to maintain.
