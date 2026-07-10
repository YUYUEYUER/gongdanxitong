describe('Customer registration', () => {
  it('registers a new customer immediately and rejects the same email', () => {
    const email = `customer-${Date.now()}@example.com`
    const password = 'StrongPass!123'

    cy.intercept('POST', '**/api/v1/customer/auth/register').as('registerCustomer')
    cy.visit('/portal/register')
    cy.get('#customer-first-name').type('Security')
    cy.get('#customer-last-name').type('Test')
    cy.get('#customer-register-email').type(email)
    cy.get('#customer-register-password').type(password, { log: false })
    cy.get('#customer-register-confirm').type(password, { log: false })
    cy.get('button[type="submit"]').click()

    cy.wait('@registerCustomer').its('response.statusCode').should('eq', 200)
    cy.url().should('include', '/portal/tickets')

    cy.clearCookies()
    cy.clearLocalStorage()
    cy.visit('/portal/register')
    cy.get('#customer-first-name').type('Duplicate')
    cy.get('#customer-register-email').type(email)
    cy.get('#customer-register-password').type(password, { log: false })
    cy.get('#customer-register-confirm').type(password, { log: false })
    cy.get('button[type="submit"]').click()

    cy.wait('@registerCustomer').its('response.statusCode').should('eq', 400)
    cy.url().should('include', '/portal/register')
  })
})
