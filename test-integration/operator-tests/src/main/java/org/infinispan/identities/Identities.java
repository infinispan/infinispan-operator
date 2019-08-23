package org.infinispan.identities;

import java.util.List;

import lombok.Getter;
import lombok.Setter;

@Getter
@Setter
public class Identities {
   private List<Credentials> credentials;

   public Credentials getCredentials(String username) {
      return credentials.stream().filter(x -> x.getUsername().equals(username)).findFirst().orElseThrow(() -> new IllegalStateException("No " + username + " user present in generated credentials!"));
   }
}
