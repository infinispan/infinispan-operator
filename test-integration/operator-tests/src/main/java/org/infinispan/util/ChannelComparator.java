package org.infinispan.util;

import java.util.Arrays;
import java.util.Comparator;

/**
 * Compares x.y channels
 */
public class ChannelComparator implements Comparator<String> {

    @Override
    public int compare(String chA, String chB) {
        int[] channelA = Arrays.stream(chA.split("\\.")).limit(2).mapToInt(Integer::parseInt).toArray();
        int[] channelB = Arrays.stream(chB.split("\\.")).limit(2).mapToInt(Integer::parseInt).toArray();

        if(channelA[0] == channelB[0]) {
            return channelA[1] -channelB[1];
        }
        return channelA[0] - channelB[0];
    }
}
