package org.infinispan.identities;

import lombok.Getter;
import lombok.Setter;

@Getter
@Setter
public class Credentials {
   private String username;
   private String password;
}
