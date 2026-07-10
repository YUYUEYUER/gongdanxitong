// ***********************************************
// This example commands.js shows you how to
// create various custom commands and overwrite
// existing commands.
//
// For more comprehensive examples of custom
// commands please read more here:
// https://on.cypress.io/custom-commands
// ***********************************************
//
//
// -- This is a parent command --
// Cypress.Commands.add('login', (email, password) => { ... })
//
//
// -- This is a child command --
// Cypress.Commands.add('drag', { prevSubject: 'element'}, (subject, options) => { ... })
//
//
// -- This is a dual command --
// Cypress.Commands.add('dismiss', { prevSubject: 'optional'}, (subject, options) => { ... })
//
//
// -- This will overwrite an existing command --
// Cypress.Commands.overwrite('visit', (originalFn, url, options) => { ... })

// Real login as the System user. Caches the session (and the csrf_token cookie
// the backend sets on login) across specs so subsequent writes don't 403.
Cypress.Commands.add('login', () => {
  const email = 'System'
  const password = Cypress.env('SYSTEM_PASSWORD') || 'StrongPass!123'

  // The production default is zh-CN, while this end-to-end journey asserts
  // the upstream English labels. Keep the test locale deterministic without
  // changing the application's persisted language setting.
  cy.intercept('GET', '**/api/v1/config', (req) => {
    req.continue((res) => {
      if (res.body?.data) {
        res.body.data['app.lang'] = 'en-US'
      }
    })
  })

  cy.session(
    'system-agent',
    () => {
      cy.visit('/')
      cy.get('#email').clear()
      cy.get('#email').type(email)
      cy.get('#password').clear()
      cy.get('#password').type(password, { log: false })
      cy.contains('button', 'Sign in').click()
      cy.url().should('include', '/inboxes')
    },
    {
      cacheAcrossSpecs: true,
      validate() {
        cy.request('/api/v1/agents/me').its('status').should('eq', 200)
      }
    }
  )
})

// Pick an option from a shadcn/radix Select by the label currently on its trigger.
Cypress.Commands.add('selectOption', (triggerLabel, optionText) => {
  cy.contains('button[role="combobox"]', triggerLabel).click()
  cy.get('[role="option"]').contains(optionText).click()
})
