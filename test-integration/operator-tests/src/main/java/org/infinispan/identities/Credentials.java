package org.infinispan.identities;

import lombok.Getter;
import lombok.Setter;

import java.util.List;

@Getter
@Setter
public class Credentials {
   private String username;
   private String password;
   private List<String> roles;
}
