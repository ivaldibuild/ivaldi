import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { API_BASE_URL } from '../api-base-url';
import { CurrentUser } from './current-user';
import { Me } from './models';

function me(roles: Me['roles']): Me {
  return {
    principal: {
      id: 'prn_ama',
      kind: 'user',
      display_name: 'Ama Mensah',
      email: 'ama@northwind.dev',
    },
    organization: { id: 'org_1', slug: 'northwind', display_name: 'Northwind', logo_url: null },
    roles,
  };
}

describe('CurrentUser', () => {
  async function load(profile: Me): Promise<CurrentUser> {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
      ],
    });
    const user = TestBed.inject(CurrentUser);
    TestBed.inject(ApplicationRef).tick();
    TestBed.inject(HttpTestingController)
      .expectOne('https://cp.test/v1/me')
      .flush({ status: 'success', data: profile });
    await new Promise((resolve) => setTimeout(resolve));
    return user;
  }

  it('describes the person from /me', async () => {
    const user = await load(me([{ role: 'member', role_id: 'r_m', resource_id: 'org_1' }]));

    expect(user.displayName()).toBe('Ama Mensah');
    expect(user.firstName()).toBe('Ama');
    expect(user.initials()).toBe('AM');
    expect(user.organization()?.display_name).toBe('Northwind');
  });

  it('takes the strongest built-in role on the Organisation itself', async () => {
    const user = await load(
      me([
        { role: 'member', role_id: 'r_m', resource_id: 'org_1' },
        { role: 'admin', role_id: 'r_a', resource_id: 'org_1' },
        { role: 'owner', role_id: 'r_o', resource_id: 'wsp_other' },
      ]),
    );

    expect(user.role()).toBe('admin');
    expect(user.roleLabel()).toBe('Admin');
    expect(user.canManageOrganization()).toBe(true);
  });

  it('does not let members or viewers manage the Organisation', async () => {
    const user = await load(me([{ role: 'viewer', role_id: 'r_v', resource_id: 'org_1' }]));

    expect(user.canManageOrganization()).toBe(false);
  });
});
